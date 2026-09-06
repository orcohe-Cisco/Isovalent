package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/investigate"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/policy"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

// investigate runs a unified search across stored Hubble flows and Tetragon
// events. Every filter is also returned as a facet with counts, which is what
// makes the UI clickable rather than a form you have to fill in from memory.
func (s *Server) investigate(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	win := investigate.ParseWindow(q.Get("window"), parseTime(q.Get("since")), parseTime(q.Get("until")))

	f := investigate.Filter{
		Sources:     csvParam(q, "source"),
		Since:       win.Since,
		Until:       win.Until,
		Namespaces:  csvParam(q, "namespace"),
		Src:         csvParam(q, "src"),
		Dst:         csvParam(q, "dst"),
		Verdicts:    csvParam(q, "verdict"),
		Policies:    csvParam(q, "policy"),
		Binaries:    csvParam(q, "binary"),
		Workloads:   csvParam(q, "workload"),
		Nodes:       csvParam(q, "node"),
		L7Types:     csvParam(q, "l7type"),
		L7Methods:   csvParam(q, "method"),
		L7Path:      q.Get("path"),
		Query:       q.Get("q"),
		BlockedOnly: q.Get("blocked") == "true",
		HideReplies: q.Get("replies") != "true",
		Limit:       queryInt(req, "limit", 200),
		Scan:        queryInt(req, "scan", 20000),
	}

	res, err := investigate.Search(req.Context(), s.store, f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	res.Window = win

	// Namespace-scoped identities must not see other namespaces' traffic.
	id := auth.FromContext(req.Context())
	if !id.IsAdmin() {
		kept := res.Rows[:0]
		for _, row := range res.Rows {
			if id.CanRead(row.Namespace) {
				kept = append(kept, row)
			}
		}
		res.Rows = kept
	}
	writeJSON(w, http.StatusOK, res)
}

func csvParam(q map[string][]string, key string) []string {
	var out []string
	for _, raw := range q[key] {
		for _, part := range strings.Split(raw, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// explainRecord answers "why was this blocked?" for one record: it loads the
// policy the agent attributed the verdict to and reports the specific clause
// the event satisfied, alongside the manifest.
func (s *Server) explainRecord(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	source := q.Get("source")
	name := q.Get("policy")
	ns := q.Get("policyNamespace")
	if name == "" {
		writeErr(w, http.StatusBadRequest, errors.New("policy is required"))
		return
	}
	raw := q.Get("raw")
	if raw == "" {
		writeErr(w, http.StatusBadRequest, errors.New("raw record is required"))
		return
	}

	switch source {
	case investigate.SourceTetragon:
		var e tetragon.Event
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		kind := k8s.KindTP
		if ns != "" {
			kind = k8s.KindTPN
		}
		p, err := s.policies.Get(req.Context(), kind, ns, name)
		if err != nil {
			writeErr(w, statusOf(err), err)
			return
		}
		writeJSON(w, http.StatusOK, k8s.ExplainTracingMatch(kind, p.Manifest, e))
	default:
		var fl hubble.Flow
		if err := json.Unmarshal([]byte(raw), &fl); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		kind := k8s.KindCNP
		if ns == "" {
			kind = k8s.KindCCNP
		}
		p, err := s.policies.Get(req.Context(), kind, ns, name)
		if err != nil {
			// Fall back to the other Cilium kind before giving up: the flow
			// only tells us the name, not whether it was cluster-wide.
			other := k8s.KindCCNP
			if kind == k8s.KindCCNP {
				other = k8s.KindCNP
			}
			p, err = s.policies.Get(req.Context(), other, ns, name)
			if err != nil {
				writeErr(w, statusOf(err), err)
				return
			}
			kind = other
		}
		effect := "allowed"
		if strings.EqualFold(fl.Verdict, "DROPPED") {
			effect = "denied"
		}
		writeJSON(w, http.StatusOK, k8s.ExplainNetworkMatch(kind, p.Manifest, fl, effect))
	}
}

// dryRunTracing replays stored Tetragon history through a proposed
// TracingPolicy. This is the "would this break anything?" button, and it is
// the only honest way to preview a Sigkill policy.
func (s *Server) dryRunTracing(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	win := investigate.ParseWindow(req.URL.Query().Get("window"), time.Time{}, time.Time{})
	events := s.historyEvents(req, win.Since, win.Until, queryInt(req, "scan", 20000))
	res := policy.SimulateTracing(body, events)
	res.Window.Since, res.Window.Until = win.Since, win.Until
	writeJSON(w, http.StatusOK, res)
}

// historyEvents loads stored Tetragon events for the window, falling back to
// the in-memory ring when no history has accumulated yet.
func (s *Server) historyEvents(req *http.Request, since, until time.Time, limit int) []tetragon.Event {
	if s.store == nil {
		return s.agg.RecentEvents(limit)
	}
	recs, err := s.store.Query(req.Context(), store.KindEvent, since, until, limit)
	if err != nil || len(recs) == 0 {
		return s.agg.RecentEvents(limit)
	}
	out := make([]tetragon.Event, 0, len(recs))
	for _, r := range recs {
		var e tetragon.Event
		if json.Unmarshal(r.Payload, &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

var _ = http.StatusOK
