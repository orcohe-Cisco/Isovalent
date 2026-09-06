package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/alerts"
	"github.com/isovalent-control/isovalent-control/backend/internal/audit"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/investigate"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/logbuf"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/version"
)

// record writes one audit entry. Every mutating handler calls it, including on
// the denied and error paths — an audit log that only records successes tells
// you nothing about the interesting afternoon.
func (s *Server) record(req *http.Request, action, target, outcome, message string, before, after json.RawMessage) {
	if s.audit == nil {
		return
	}
	id := auth.FromContext(req.Context())
	roles := make([]string, 0, len(id.Roles))
	for _, r := range id.Roles {
		roles = append(roles, string(r.Name))
	}
	actor := id.Subject
	if id.Email != "" {
		actor = id.Email
	}
	e := audit.Entry{
		Actor: actor, Roles: roles, Source: clientIP(req),
		Action: action, Target: target, Outcome: outcome, Message: message,
		Before: before, After: after,
	}
	if len(before) > 0 || len(after) > 0 {
		e.Diff = audit.DiffManifests(before, after)
	}
	s.audit.Record(e)
}

func clientIP(req *http.Request) string {
	if ip := req.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	return req.RemoteAddr
}

// --- UI configuration ----------------------------------------------------

// getUIConfig tells the frontend which features are actually wired up, so the
// menu can hide what is not configured instead of offering a page that will
// only ever show an error.
func (s *Server) getUIConfig(w http.ResponseWriter, req *http.Request) {
	aiStatus := map[string]any{"enabled": false}
	if s.ai != nil {
		aiStatus = map[string]any{
			"enabled":  s.ai.Status().Enabled,
			"provider": s.ai.Status().Provider,
			"model":    s.ai.Status().Model,
			"reason":   s.ai.Status().Reason,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cluster":       s.cfg.ClusterName,
		"mode":          "live",
		"version":       version.Version,
		"commit":        version.Commit,
		"built":         version.Date,
		"uptimeSeconds": int(time.Since(s.started).Seconds()),
		"features": map[string]any{
			"hubbleUI": s.hubbleUI != nil,
			"grafana":  s.grafana != nil,
			"gitops":   s.gitops != nil && s.gitops.Enabled(),
			"ai":       aiStatus,
			"history":  s.store != nil,
			"auth":     s.verifier != nil,
			"database": s.cfg.DBDSN != "",
		},
		"embeds": map[string]string{
			"hubbleUI": "/hubble-ui/",
			"grafana":  "/grafana/",
		},
		"grafanaDashboardUid": s.cfg.GrafanaDashboardUID,
		"protectedNamespaces": s.guard.Protected(),
		"selfNamespace":       s.cfg.SelfNamespace,
		"policyKinds":         k8s.AllKinds,
		"alertSinks":          alerts.Specs(),
		"windows":             []string{"5m", "15m", "1h", "6h", "1d", "7d", "30d"},
	})
}

// getNamespaces lists cluster namespaces for the pickers, flagging the ones
// policies authored here may not target.
func (s *Server) getNamespaces(w http.ResponseWriter, req *http.Request) {
	if s.k8s == nil {
		writeJSON(w, http.StatusOK, []k8s.NamespaceInfo{})
		return
	}
	list, err := s.k8s.Namespaces(req.Context())
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	id := auth.FromContext(req.Context())
	out := make([]k8s.NamespaceInfo, 0, len(list))
	for _, ns := range list {
		if !id.CanRead(ns.Name) {
			continue
		}
		ns.Protected = s.guard.IsProtected(ns.Name)
		out = append(out, ns)
	}
	writeJSON(w, http.StatusOK, out)
}

// --- audit ---------------------------------------------------------------

func (s *Server) getAudit(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	if !id.IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("the audit log requires the admin role"))
		return
	}
	if s.audit == nil {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []audit.Entry{}})
		return
	}
	win := investigate.ParseWindow(req.URL.Query().Get("window"),
		parseTime(req.URL.Query().Get("since")), parseTime(req.URL.Query().Get("until")))
	entries := s.audit.Query(req.Context(), win.Since, win.Until, req.URL.Query().Get("q"), queryInt(req, "limit", 200))
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries, "stats": s.audit.Stats(), "window": win,
	})
}

// --- logs ----------------------------------------------------------------

func (s *Server) getLogs(w http.ResponseWriter, req *http.Request) {
	if s.logs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []logbuf.Line{}})
		return
	}
	q := req.URL.Query()
	writeJSON(w, http.StatusOK, map[string]any{
		"lines":  s.logs.Query(q.Get("level"), q.Get("origin"), q.Get("q"), queryInt(req, "limit", 500)),
		"counts": s.logs.Counts(),
	})
}

// postClientLog ingests a log line from the browser, so frontend and backend
// problems land in one timeline. Without this, half the story is in a devtools
// console nobody has open.
func (s *Server) postClientLog(w http.ResponseWriter, req *http.Request) {
	if s.logs == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var body struct {
		Lines []struct {
			Time    time.Time         `json:"time"`
			Level   string            `json:"level"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		} `json:"lines"`
	}
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	for _, l := range body.Lines {
		s.logs.Append(logbuf.Line{
			Time: l.Time, Level: strings.ToUpper(orDefault(l.Level, "INFO")),
			Origin: "frontend", Message: l.Message, Fields: l.Fields,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// streamLogs pushes new log lines over the same WebSocket hub the flow and
// event streams use. The log buffer publishes into the "logs" topic.
func (s *Server) streamLogs(w http.ResponseWriter, req *http.Request) {
	s.hub.ServeWS("logs")(w, req)
}

// --- diagnostics ---------------------------------------------------------

// Check is one connectivity probe result.
type Check struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"` // ok | degraded | down | disabled
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Detail    string `json:"detail,omitempty"`
	// Hint is the concrete next step when the check fails. A diagnostic that
	// says "error" and stops has moved the problem, not solved it.
	Hint string `json:"hint,omitempty"`
	// Target is what was probed, so the answer is not "something is broken".
	Target string `json:"target,omitempty"`
}

// getDiagnostics probes every dependency in parallel and reports which leg is
// broken. This is the page to open when a table is unexpectedly empty.
func (s *Server) getDiagnostics(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer cancel()

	checks := make([]Check, 7)
	var wg sync.WaitGroup
	wg.Add(7)

	go func() {
		defer wg.Done()
		c := Check{ID: "kubernetes", Name: "Kubernetes API", Target: orDefault(s.cfg.K8sAPIServer, "in-cluster / kubectl proxy")}
		start := time.Now()
		if v, err := s.k8s.ServerVersion(ctx); err != nil {
			c.Status, c.Detail = "down", err.Error()
			c.Hint = "The console cannot read or write policy. Check the ServiceAccount RBAC, or run `kubectl proxy` if you are running the backend outside the cluster."
		} else {
			c.Status, c.Detail = "ok", v
		}
		c.LatencyMs = time.Since(start).Milliseconds()
		checks[0] = c
	}()

	go func() {
		defer wg.Done()
		o := s.agg.Overview()
		c := Check{ID: "hubble", Name: "Hubble Relay", Target: s.cfg.HubbleRelayAddr}
		switch {
		case o.TotalFlows > 0 && o.FlowRate > 0:
			c.Status, c.Detail = "ok", "receiving flows"
		case o.TotalFlows > 0:
			c.Status, c.Detail = "degraded", "flows seen earlier but none in the last minute"
			c.Hint = "Either the cluster is idle, or the relay stream dropped. Check the backend log for 'hubble stream interrupted'."
		default:
			c.Status, c.Detail = "down", "no flows received since start"
			c.Hint = "Enable Hubble and the relay (`cilium hubble enable --relay`), then point IC_HUBBLE_RELAY_ADDR at hubble-relay:80 or a port-forward on :4245."
		}
		checks[1] = c
	}()

	go func() {
		defer wg.Done()
		o := s.agg.Overview()
		c := Check{ID: "tetragon", Name: "Tetragon", Target: s.cfg.TetragonAddr}
		if o.TotalEvents > 0 {
			c.Status, c.Detail = "ok", "receiving runtime events"
		} else {
			c.Status, c.Detail = "down", "no events received since start"
			c.Hint = "Install Tetragon and expose its gRPC service, then set IC_TETRAGON_ADDR (tetragon:54321 in-cluster, or a port-forward)."
		}
		checks[2] = c
	}()

	go func() {
		defer wg.Done()
		c := Check{ID: "hubble-ui", Name: "Hubble UI (embedded map)", Target: s.cfg.HubbleUIURL}
		if s.hubbleUI == nil {
			c.Status, c.Hint = "disabled", "Set IC_HUBBLE_UI_URL to the hubble-ui service."
		} else if ok, lat, detail := s.hubbleUI.Probe(ctx); ok {
			c.Status, c.Detail, c.LatencyMs = "ok", detail, lat.Milliseconds()
		} else {
			c.Status, c.Detail, c.LatencyMs = "down", detail, lat.Milliseconds()
			c.Hint = "The Service Map page embeds this. Install hubble-ui (`cilium hubble enable --ui`) or point IC_HUBBLE_UI_URL at it."
		}
		checks[3] = c
	}()

	go func() {
		defer wg.Done()
		c := Check{ID: "grafana", Name: "Grafana (embedded dashboards)", Target: s.cfg.GrafanaURL}
		if s.grafana == nil {
			c.Status, c.Hint = "disabled", "Set IC_GRAFANA_URL to your Grafana service."
		} else if ok, lat, detail := s.grafana.Probe(ctx); ok {
			c.Status, c.Detail, c.LatencyMs = "ok", detail, lat.Milliseconds()
		} else {
			c.Status, c.Detail, c.LatencyMs = "down", detail, lat.Milliseconds()
			c.Hint = "Run the installer with Grafana enabled, or point IC_GRAFANA_URL at an existing instance and import deploy/observability/dashboards/."
		}
		checks[4] = c
	}()

	go func() {
		defer wg.Done()
		c := Check{ID: "history", Name: "History store"}
		if s.cfg.DBDSN != "" {
			c.Target = "postgres"
		} else {
			c.Target = "in-memory ring"
		}
		if s.store == nil {
			c.Status, c.Detail = "down", "no store configured"
		} else if _, err := s.store.Query(ctx, store.KindFlow, time.Now().Add(-time.Minute), time.Now(), 1); err != nil {
			c.Status, c.Detail = "degraded", err.Error()
			c.Hint = "Historical Investigation will be empty. Check IC_DB_DSN."
		} else {
			c.Status = "ok"
			if s.cfg.DBDSN == "" {
				c.Detail = "in-memory only — history is lost on restart. Set IC_DB_DSN for durable history."
			} else {
				c.Detail = "postgres reachable"
			}
		}
		checks[5] = c
	}()

	go func() {
		defer wg.Done()
		c := Check{ID: "ai", Name: "AI assistant"}
		if s.ai == nil {
			c.Status = "disabled"
		} else {
			st := s.ai.Status()
			c.Target = st.Provider
			if st.Enabled {
				c.Status, c.Detail = "ok", st.Provider+" / "+st.Model
			} else {
				c.Status, c.Detail, c.Hint = "disabled", st.Reason, "Set IC_AI_PROVIDER and mount a key via IC_AI_API_KEY_FILE."
			}
		}
		checks[6] = c
	}()

	wg.Wait()

	worst := "ok"
	for _, c := range checks {
		switch c.Status {
		case "down":
			worst = "down"
		case "degraded":
			if worst != "down" {
				worst = "degraded"
			}
		}
	}
	logCounts := map[string]int64{}
	if s.logs != nil {
		logCounts = s.logs.Counts()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": worst, "checks": checks, "checkedAt": time.Now().UTC(),
		"logCounts":     logCounts,
		"uptimeSeconds": int(time.Since(s.started).Seconds()),
	})
}
