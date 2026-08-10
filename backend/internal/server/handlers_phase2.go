package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/isovalent-control/isovalent-control/backend/internal/alerts"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/policy"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
)

// --- dry-run ------------------------------------------------------------

func (s *Server) dryRunPolicy(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	limit := queryInt(req, "flows", 500)
	res := policy.Simulate(body, s.agg.RecentFlows(limit))
	writeJSON(w, http.StatusOK, res)
}

// --- tetragon policy list + kill/monitor toggle -------------------------

func (s *Server) listTracingPolicies(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	out := []k8s.TracingPolicyInfo{}
	for _, kind := range []k8s.Kind{k8s.KindTP, k8s.KindTPN} {
		items, err := s.policies.List(req.Context(), kind, "")
		if err != nil {
			writeErr(w, statusOf(err), err)
			return
		}
		for _, p := range items {
			if !id.CanRead(p.Namespace) {
				continue
			}
			out = append(out, k8s.DescribeTracingPolicy(kind, p.Manifest))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setTracingAction(w http.ResponseWriter, req *http.Request) {
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

	var reqBody struct {
		Action string `json:"action"` // monitor | enforce
	}
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<16)).Decode(&reqBody); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	action := k8s.TracingAction(reqBody.Action)
	if action != k8s.ActionMonitor && action != k8s.ActionEnforce {
		writeErr(w, http.StatusBadRequest, errors.New("action must be 'monitor' or 'enforce'"))
		return
	}

	cur, err := s.policies.Get(req.Context(), kind, ns, name)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	mutated, err := k8s.SetTracingAction(cur.Manifest, action)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}

	// GitOps PR mode instead of live apply when requested + configured.
	if req.URL.Query().Get("mode") == "pr" {
		s.applyViaPR(w, req, kind, name, mutated, "set "+name+" to "+reqBody.Action)
		return
	}
	applied, err := s.policies.Apply(req.Context(), kind, ns, name, mutated)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, k8s.DescribeTracingPolicy(kind, applied.Manifest))
}

// --- alert routes -------------------------------------------------------

func (s *Server) getAlertRoutes(w http.ResponseWriter, req *http.Request) {
	if s.router == nil {
		writeJSON(w, http.StatusOK, []alerts.Route{})
		return
	}
	writeJSON(w, http.StatusOK, s.router.Routes())
}

func (s *Server) setAlertRoutes(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	if !id.IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("alert routing requires the admin role"))
		return
	}
	if s.router == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("alert router not initialized"))
		return
	}
	var routes []alerts.Route
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<20)).Decode(&routes); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.router.SetRoutes(routes)
	writeJSON(w, http.StatusOK, s.router.Routes())
}

func (s *Server) testAlertRoute(w http.ResponseWriter, req *http.Request) {
	if s.router == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("alert router not initialized"))
		return
	}
	var route alerts.Route
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<16)).Decode(&route); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.router.SendTest(route); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "delivered"})
}

// --- history ------------------------------------------------------------

func (s *Server) queryHistory(w http.ResponseWriter, req *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusOK, []store.Record{})
		return
	}
	kind := chi.URLParam(req, "kind")
	if kind != store.KindFlow && kind != store.KindEvent && kind != store.KindAlert {
		writeErr(w, http.StatusBadRequest, errors.New("kind must be flow, event, or alert"))
		return
	}
	since := parseTime(req.URL.Query().Get("since"))
	until := parseTime(req.URL.Query().Get("until"))
	recs, err := s.store.Query(req.Context(), kind, since, until, queryInt(req, "limit", 200))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func (s *Server) gitopsRepo() string {
	if s.gitops == nil {
		return ""
	}
	return s.gitops.Repo
}

// applyViaPR renders a manifest to a file and opens a GitHub PR instead of
// applying live (ArgoCD/Flux reconcile it). The committed content is indented
// JSON, which is valid YAML.
func (s *Server) applyViaPR(w http.ResponseWriter, req *http.Request, kind k8s.Kind, name string, manifest json.RawMessage, summary string) {
	if s.gitops == nil || !s.gitops.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, errors.New("GitOps PR mode not configured (set IC_GITHUB_REPO and IC_GITHUB_TOKEN)"))
		return
	}
	var obj any
	if err := json.Unmarshal(manifest, &obj); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	pretty, _ := json.MarshalIndent(obj, "", "  ")
	filename := strings.ToLower(string(kind)) + "-" + name + ".yaml"
	url, err := s.gitops.OpenPR(req.Context(), filename, string(pretty)+"\n", summary,
		"Automated policy change proposed by isovalent-control.")
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"pullRequest": url, "mode": "gitops-pr"})
}
