// Package server wires the REST + WebSocket API.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/isovalent-control/isovalent-control/backend/internal/alerts"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/config"
	"github.com/isovalent-control/isovalent-control/backend/internal/gitops"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/stream"
)

// Server hosts the HTTP API.
type Server struct {
	cfg      config.Config
	hub      *stream.Hub
	agg      *Aggregator
	policies k8s.PolicyStore
	verifier *auth.Verifier // nil = auth disabled (dev mode)
	router   *alerts.Router
	store    store.Store
	gitops   *gitops.Client
}

// Deps bundles the optional Phase-2 components.
type Deps struct {
	Router *alerts.Router
	Store  store.Store
	GitOps *gitops.Client
}

// New assembles a Server.
func New(cfg config.Config, hub *stream.Hub, agg *Aggregator, policies k8s.PolicyStore, verifier *auth.Verifier, deps Deps) *Server {
	return &Server{
		cfg: cfg, hub: hub, agg: agg, policies: policies, verifier: verifier,
		router: deps.Router, store: deps.Store, gitops: deps.GitOps,
	}
}

// Router builds the chi mux.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(s.cors)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": string(s.cfg.Mode)})
	})

	// Prometheus metrics (unauthenticated, for scraping).
	r.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		s.agg.WriteMetrics(w)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.verifier.Middleware)

		r.Get("/me", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, auth.FromContext(req.Context()))
		})
		r.Get("/overview", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"cluster":  s.cfg.ClusterName,
				"mode":     s.cfg.Mode,
				"overview": s.agg.Overview(),
				"alerts":   s.agg.RecentAlerts(25),
			})
		})
		r.Get("/servicemap", func(w http.ResponseWriter, req *http.Request) {
			nodes, edges := s.agg.ServiceMap()
			writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "edges": edges})
		})
		r.Get("/flows/recent", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, s.agg.RecentFlows(queryInt(req, "limit", 100)))
		})
		r.Get("/events/recent", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, s.agg.RecentEvents(queryInt(req, "limit", 100)))
		})
		r.Get("/alerts/recent", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, s.agg.RecentAlerts(queryInt(req, "limit", 50)))
		})

		r.Route("/policies/{kind}", func(r chi.Router) {
			r.Get("/", s.listPolicies)
			r.Route("/{namespace}/{name}", func(r chi.Router) {
				r.Get("/", s.getPolicy)
				r.Put("/", s.applyPolicy)
				r.Delete("/", s.deletePolicy)
			})
		})

		// Dry-run: simulate a proposed policy against recent flows.
		r.Post("/policies/dryrun", s.dryRunPolicy)

		// Tetragon runtime policies: organized list + kill/monitor toggle.
		r.Get("/tracingpolicies", s.listTracingPolicies)
		r.Post("/tracingpolicies/{namespace}/{name}/action", s.setTracingAction)

		// Alert routing.
		r.Get("/alerts/routes", s.getAlertRoutes)
		r.Put("/alerts/routes", s.setAlertRoutes)
		r.Post("/alerts/routes/test", s.testAlertRoute)

		// Historical (time-travel) queries.
		r.Get("/history/{kind}", s.queryHistory)

		// GitOps status (whether PR apply mode is available).
		r.Get("/gitops/status", func(w http.ResponseWriter, _ *http.Request) {
			enabled := s.gitops != nil && s.gitops.Enabled()
			writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "repo": s.gitopsRepo()})
		})

		// UI config — where the embedded Hubble UI + Grafana live, feature flags.
		r.Get("/config", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"cluster":             s.cfg.ClusterName,
				"mode":                s.cfg.Mode,
				"hubbleUiUrl":         s.cfg.HubbleUIURL,
				"grafanaUrl":          s.cfg.GrafanaURL,
				"grafanaDashboardUid": s.cfg.GrafanaDashboardUID,
				"retentionDays":       s.cfg.RetentionDays,
				"gitops":              map[string]any{"enabled": s.gitops != nil && s.gitops.Enabled(), "repo": s.gitopsRepo()},
			})
		})
	})

	// WebSocket streams (token via ?access_token= for browser clients).
	r.Route("/ws", func(r chi.Router) {
		r.Use(s.verifier.Middleware)
		r.Get("/flows", s.hub.ServeWS("flows"))
		r.Get("/events", s.hub.ServeWS("events"))
		r.Get("/alerts", s.hub.ServeWS("alerts"))
	})

	return r
}

// --- policy handlers ---------------------------------------------------

// kindParam resolves the {kind} URL segment. Accepts both the short group
// ("network" | "tracing") plus explicit CRD kind names.
func kindParam(req *http.Request) ([]k8s.Kind, error) {
	switch chi.URLParam(req, "kind") {
	case "network":
		return []k8s.Kind{k8s.KindCNP, k8s.KindCCNP}, nil
	case "tracing":
		return []k8s.Kind{k8s.KindTP, k8s.KindTPN}, nil
	default:
		k, err := k8s.ParseKind(chi.URLParam(req, "kind"))
		if err != nil {
			return nil, err
		}
		return []k8s.Kind{k}, nil
	}
}

// nsParam maps the reserved segment "-" to cluster scope.
func nsParam(req *http.Request) string {
	ns := chi.URLParam(req, "namespace")
	if ns == "-" {
		return ""
	}
	return ns
}

// resolveKind picks the matching kind for the addressed object: namespaced
// kinds for real namespaces, cluster-scoped kinds for "-".
func resolveKind(kindsList []k8s.Kind, namespace string) k8s.Kind {
	for _, k := range kindsList {
		if k.Namespaced() == (namespace != "") {
			return k
		}
	}
	return kindsList[0]
}

func (s *Server) listPolicies(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	kindsList, err := kindParam(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	nsFilter := req.URL.Query().Get("namespace")
	var out []k8s.Policy
	for _, kind := range kindsList {
		items, err := s.policies.List(req.Context(), kind, nsFilter)
		if err != nil {
			writeErr(w, statusOf(err), err)
			return
		}
		for _, p := range items {
			if id.CanRead(p.Namespace) {
				out = append(out, p)
			}
		}
	}
	if out == nil {
		out = []k8s.Policy{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getPolicy(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	kindsList, err := kindParam(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ns := nsParam(req)
	if !id.CanRead(ns) {
		writeErr(w, http.StatusForbidden, errors.New("not authorized for namespace "+ns))
		return
	}
	p, err := s.policies.Get(req.Context(), resolveKind(kindsList, ns), ns, chi.URLParam(req, "name"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) applyPolicy(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	kindsList, err := kindParam(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ns := nsParam(req)
	name := chi.URLParam(req, "name")
	kind := resolveKind(kindsList, ns)

	if ns == "" && !id.IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("cluster-scoped policies require the admin role"))
		return
	}
	if ns != "" && !id.CanEdit(ns) {
		writeErr(w, http.StatusForbidden, errors.New("not authorized to edit policies in namespace "+ns))
		return
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := k8s.ValidateManifest(kind, ns, name, body); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	// GitOps PR mode: open a PR instead of applying live.
	if req.URL.Query().Get("mode") == "pr" {
		s.applyViaPR(w, req, kind, name, body, "apply "+string(kind)+"/"+name)
		return
	}
	p, err := s.policies.Apply(req.Context(), kind, ns, name, body)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deletePolicy(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	kindsList, err := kindParam(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ns := nsParam(req)
	if ns == "" && !id.IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("cluster-scoped policies require the admin role"))
		return
	}
	if ns != "" && !id.CanEdit(ns) {
		writeErr(w, http.StatusForbidden, errors.New("not authorized to edit policies in namespace "+ns))
		return
	}
	if err := s.policies.Delete(req.Context(), resolveKind(kindsList, ns), ns, chi.URLParam(req, "name")); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ------------------------------------------------------------

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := s.cfg.CORSOrigin
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func statusOf(err error) int {
	var apiErr *k8s.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return http.StatusBadGateway
}

func queryInt(req *http.Request, key string, def int) int {
	if v := req.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			return n
		}
	}
	return def
}
