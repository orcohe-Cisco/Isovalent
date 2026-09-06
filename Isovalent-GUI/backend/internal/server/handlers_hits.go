package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/isovalent-control/isovalent-control/backend/internal/audit"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/hits"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
)

// listHits returns one row per TracingPolicy that has produced events, joined
// with what the cluster says about that policy (mode, hooks, whether the
// console manages it). A policy the agent reports but the API server does not
// have is still listed — that mismatch is itself worth seeing.
func (s *Server) listHits(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	summaries := []hits.Summary{}
	if s.hits != nil {
		summaries = s.hits.Summaries()
	}

	// Index the live policies so each row can carry its current action.
	type meta struct {
		Kind     k8s.Kind `json:"kind"`
		Action   string   `json:"action"`
		Category string   `json:"category,omitempty"`
		Hooks    []string `json:"hooks,omitempty"`
		Managed  bool     `json:"managed"`
		Present  bool     `json:"present"`
	}
	index := map[string]meta{}
	for _, kind := range []k8s.Kind{k8s.KindTP, k8s.KindTPN} {
		items, err := s.policies.List(req.Context(), kind, "")
		if err != nil {
			continue // reported by /diagnostics; do not fail the whole page
		}
		for _, p := range items {
			if !id.CanRead(p.Namespace) {
				continue
			}
			info := k8s.DescribeTracingPolicy(kind, p.Manifest)
			key := p.Name
			if p.Namespace != "" {
				key = p.Namespace + "/" + p.Name
			}
			index[key] = meta{
				Kind: kind, Action: string(info.Action), Category: info.Category,
				Hooks: info.Hooks, Managed: info.Managed, Present: true,
			}
		}
	}

	type row struct {
		hits.Summary
		meta
	}
	out := make([]row, 0, len(summaries))
	for _, sum := range summaries {
		if !id.CanRead(sum.Namespace) {
			continue
		}
		key := sum.Policy
		if sum.Namespace != "" {
			key = sum.Namespace + "/" + sum.Policy
		}
		out = append(out, row{Summary: sum, meta: index[key]})
	}

	// Policies that exist but have not fired belong in the list too: "zero
	// hits" is a finding, not an absence of data.
	seen := map[string]bool{}
	for _, r := range out {
		key := r.Policy
		if r.Namespace != "" {
			key = r.Namespace + "/" + r.Policy
		}
		seen[key] = true
	}
	for key, m := range index {
		if seen[key] {
			continue
		}
		ns, name := splitKey(key)
		out = append(out, row{
			Summary: hits.Summary{Policy: name, Namespace: ns, Unique: map[string]int{}},
			meta:    m,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"policies":   out,
		"dimensions": hits.Dimensions,
		"excludable": excludableDimensions(),
	})
}

func excludableDimensions() []string {
	var out []string
	for _, d := range hits.Dimensions {
		if hits.Excludable(d) {
			out = append(out, d)
		}
	}
	return out
}

func splitKey(key string) (namespace, name string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:]
		}
	}
	return "", key
}

// getHitDetail returns the per-dimension breakdown plus the policy manifest,
// so the UI can show what fired it next to the rule that fired.
func (s *Server) getHitDetail(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	ns := nsParam(req)
	name := chi.URLParam(req, "name")
	if !id.CanRead(ns) {
		writeErr(w, http.StatusForbidden, errors.New("not authorized for namespace "+ns))
		return
	}
	detail, ok := hits.Detail{}, false
	if s.hits != nil {
		detail, ok = s.hits.Detail(ns, name)
	}
	if !ok {
		detail = hits.Detail{
			Summary: hits.Summary{Policy: name, Namespace: ns, Unique: map[string]int{}},
			Values:  map[string][]hits.Value{},
		}
	}

	kind := k8s.KindTP
	if ns != "" {
		kind = k8s.KindTPN
	}
	resp := map[string]any{"detail": detail, "kind": kind, "present": true}
	if p, err := s.policies.Get(req.Context(), kind, ns, name); err == nil {
		resp["manifest"] = p.Manifest
		resp["info"] = k8s.DescribeTracingPolicy(kind, p.Manifest)
		// Explain the newest sample against the live policy so the drill-down
		// opens on "this is the clause that matched", not on raw YAML.
		if len(detail.Samples) > 0 {
			resp["explain"] = k8s.ExplainTracingMatch(kind, p.Manifest, detail.Samples[0])
		}
	} else {
		// A policy that fired in the past and no longer exists is a normal,
		// expected state — Exclusions reads from Tetragon's own event
		// history, which outlives the policy that produced it. That is not
		// an error the UI should show as one: "present: false" lets the
		// frontend render its own quiet explanation instead of a raw
		// Kubernetes API error, while still showing everything the hit
		// history actually has (samples, dimensions, counts). Reserve
		// policyError for anything else — a permissions problem or a real
		// API hiccup is worth surfacing as an error, deletion is not.
		var apiErr *k8s.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			resp["present"] = false
		} else {
			resp["policyError"] = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// applyExclusions writes approved exclusions into the live policy.
//
// With ?dryRun=true it returns the mutated manifest and the notes without
// touching the cluster, which is what the Review step shows. Nothing is ever
// applied from this endpoint without the caller having seen that preview.
func (s *Server) applyExclusions(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	ns := nsParam(req)
	name := chi.URLParam(req, "name")
	kind := k8s.KindTP
	if ns != "" {
		kind = k8s.KindTPN
	}
	if ns == "" && !id.IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("cluster-scoped TracingPolicies require the admin role"))
		return
	}
	if ns != "" && !id.CanEdit(ns) {
		writeErr(w, http.StatusForbidden, errors.New("not authorized to edit namespace "+ns))
		return
	}

	var body k8s.Exclusions
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Empty() {
		writeErr(w, http.StatusBadRequest, errors.New("no exclusions supplied"))
		return
	}

	cur, err := s.policies.Get(req.Context(), kind, ns, name)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	mutated, notes, err := k8s.ApplyExclusions(cur.Manifest, body)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	if err := s.guard.CheckManifest(mutated); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}

	if req.URL.Query().Get("dryRun") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{
			"dryRun": true, "notes": notes, "manifest": json.RawMessage(mutated),
			"diff": audit.DiffManifests(cur.Manifest, mutated),
		})
		return
	}

	if req.URL.Query().Get("mode") == "pr" {
		s.applyViaPR(w, req, kind, name, mutated, "add exclusions to "+name)
		s.record(req, "policy.exclude", string(kind)+" "+ns+"/"+name, audit.OutcomeSuccess, "opened a pull request", cur.Manifest, mutated)
		return
	}

	applied, err := s.policies.Apply(req.Context(), kind, ns, name, mutated)
	if err != nil {
		s.record(req, "policy.exclude", string(kind)+" "+ns+"/"+name, audit.OutcomeError, err.Error(), cur.Manifest, mutated)
		writeErr(w, statusOf(err), err)
		return
	}
	// Clearing the counters makes the next few minutes meaningful: if the
	// exclusion worked, the policy goes quiet.
	if s.hits != nil {
		s.hits.Reset(ns, name)
	}
	s.record(req, "policy.exclude", string(kind)+" "+ns+"/"+name, audit.OutcomeSuccess, describeExclusions(body), cur.Manifest, applied.Manifest)
	writeJSON(w, http.StatusOK, map[string]any{
		"notes": notes, "policy": applied,
		"info": k8s.DescribeTracingPolicy(kind, applied.Manifest),
	})
}

func describeExclusions(e k8s.Exclusions) string {
	parts := []string{}
	add := func(label string, vals []string) {
		for _, v := range vals {
			parts = append(parts, label+"="+v)
		}
	}
	add("binary", e.Binaries)
	add("parent", e.ParentBinaries)
	add("path", e.Paths)
	add("podLabel", e.PodLabels)
	add("container", e.Containers)
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return "excluded " + out
}

// resetHits clears the counters for one policy without changing it.
func (s *Server) resetHits(w http.ResponseWriter, req *http.Request) {
	ns := nsParam(req)
	name := chi.URLParam(req, "name")
	if s.hits != nil {
		s.hits.Reset(ns, name)
	}
	s.record(req, "hits.reset", ns+"/"+name, audit.OutcomeSuccess, "", nil, nil)
	w.WriteHeader(http.StatusNoContent)
}
