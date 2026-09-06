// Package server wires the REST + WebSocket API.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/isovalent-control/isovalent-control/backend/internal/ai"
	"github.com/isovalent-control/isovalent-control/backend/internal/alerts"
	"github.com/isovalent-control/isovalent-control/backend/internal/apikeys"
	"github.com/isovalent-control/isovalent-control/backend/internal/audit"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/config"
	"github.com/isovalent-control/isovalent-control/backend/internal/gitops"
	"github.com/isovalent-control/isovalent-control/backend/internal/guard"
	"github.com/isovalent-control/isovalent-control/backend/internal/hits"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/logbuf"
	"github.com/isovalent-control/isovalent-control/backend/internal/proxy"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/stream"
	"github.com/isovalent-control/isovalent-control/backend/internal/version"
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
	k8s      *k8s.Client // raw API client for version probes and namespaces
	versions versionCache

	audit    *audit.Log
	logs     *logbuf.Buffer
	hits     *hits.Tracker
	guard    *guard.Guard
	ai       *ai.Client
	keys     *apikeys.Store
	hubbleUI *proxy.Target
	grafana  *proxy.Target
	started  time.Time
}

// Deps bundles the components the server does not own.
type Deps struct {
	Router   *alerts.Router
	Store    store.Store
	GitOps   *gitops.Client
	K8s      *k8s.Client
	Audit    *audit.Log
	Logs     *logbuf.Buffer
	Hits     *hits.Tracker
	Guard    *guard.Guard
	AI       *ai.Client
	Keys     *apikeys.Store
	HubbleUI *proxy.Target
	Grafana  *proxy.Target
}

// New assembles a Server.
func New(cfg config.Config, hub *stream.Hub, agg *Aggregator, policies k8s.PolicyStore, verifier *auth.Verifier, deps Deps) *Server {
	return &Server{
		cfg: cfg, hub: hub, agg: agg, policies: policies, verifier: verifier,
		router: deps.Router, store: deps.Store, gitops: deps.GitOps, k8s: deps.K8s,
		audit: deps.Audit, logs: deps.Logs, hits: deps.Hits, guard: deps.Guard,
		ai: deps.AI, keys: deps.Keys, hubbleUI: deps.HubbleUI, grafana: deps.Grafana,
		started: time.Now(),
	}
}

// Resolve implements auth.TokenResolver: it maps an issued API key to an
// identity so machine clients can call the same endpoints the UI does.
func (s *Server) Resolve(token string) (*auth.Identity, bool) {
	if s.keys == nil {
		return nil, false
	}
	k, ok := s.keys.Lookup(token)
	if !ok {
		return nil, false
	}
	return &auth.Identity{
		Subject: "apikey:" + k.ID,
		Name:    k.Name,
		Roles:   []auth.Role{{Name: auth.RoleName(k.Role)}},
	}, true
}

// Router builds the chi mux.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(s.cors)

	// The version is here on purpose: `curl /healthz` is the fastest way to
	// tell whether the cluster is running the image you think it is.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"mode":    "live",
			"version": version.Version,
			"commit":  version.Commit,
			"built":   version.Date,
			"cluster": s.cfg.ClusterName,
		})
	})

	// Embedded upstream consoles. These are mounted outside /api/v1 because
	// the browser loads them in an iframe, which cannot attach a bearer
	// token; they inherit the console's own network exposure instead.
	if s.hubbleUI != nil {
		r.Handle("/hubble-ui", http.RedirectHandler("/hubble-ui/", http.StatusMovedPermanently))
		r.Handle("/hubble-ui/*", s.hubbleUI)
	}
	if s.grafana != nil {
		r.Handle("/grafana", http.RedirectHandler("/grafana/", http.StatusMovedPermanently))
		r.Handle("/grafana/*", s.grafana)
	}

	// Prometheus metrics (unauthenticated, for scraping).
	r.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		s.agg.WriteMetrics(w)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auth.Middleware(s.verifier, s))

		r.Get("/me", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, auth.FromContext(req.Context()))
		})
		// Everything the shell needs to render itself: cluster identity, which
		// integrations are wired up, and which menu entries are meaningful.
		r.Get("/config", s.getUIConfig)
		r.Get("/namespaces", s.getNamespaces)
		r.Get("/overview", func(w http.ResponseWriter, req *http.Request) {
			policies, events, enforced := 0, int64(0), int64(0)
			if s.hits != nil {
				policies, events, enforced = s.hits.Totals()
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"cluster":  s.cfg.ClusterName,
				"mode":     "live",
				"overview": s.agg.Overview(),
				"alerts":   s.agg.RecentAlerts(25),
				"hits": map[string]any{
					"policies": policies, "events": events, "enforced": enforced,
				},
			})
		})
		// Component versions (Cilium / Hubble / Tetragon / Kubernetes).
		r.Get("/versions", s.getVersions)

		// Distinct values the platform has observed, for exclusion pickers.
		r.Get("/observed", s.getObserved)

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

		// --- Exclusions: what fired each policy, and how to stop it ---
		r.Get("/hits", s.listHits)
		r.Get("/hits/{namespace}/{name}", s.getHitDetail)
		r.Post("/hits/{namespace}/{name}/exclusions", s.applyExclusions)
		r.Delete("/hits/{namespace}/{name}", s.resetHits)

		// --- Historical investigation ---
		r.Get("/investigate", s.investigate)
		r.Get("/investigate/explain", s.explainRecord)

		// --- Runtime policy dry-run against stored history ---
		r.Post("/tracingpolicies/dryrun", s.dryRunTracing)

		// --- Alert sink catalogue (field labels per integration) ---
		r.Get("/alerts/sinks", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, alerts.Specs())
		})

		// --- Audit log ---
		r.Get("/audit", s.getAudit)

		// --- Console logs + connectivity diagnostics ---
		r.Get("/logs", s.getLogs)
		r.Post("/logs/client", s.postClientLog)
		r.Get("/diagnostics", s.getDiagnostics)

		// --- AI assistant ---
		r.Get("/ai/status", s.getAIStatus)
		r.Post("/ai/analyze", s.postAIAnalyze)
		r.Post("/ai/apply", s.postAIApply)

		// --- API tokens ---
		r.Get("/apikeys", s.listAPIKeys)
		r.Post("/apikeys", s.createAPIKey)
		r.Delete("/apikeys/{id}", s.revokeAPIKey)

		// --- Deployment command catalogue ---
		r.Get("/deployment", s.getDeployment)
	})

	// The OpenAPI document, unauthenticated so the docs page renders before
	// you have a token.
	r.Get("/api/openapi.yaml", s.getOpenAPI)

	// WebSocket streams (token via ?access_token= for browser clients).
	r.Route("/ws", func(r chi.Router) {
		r.Use(auth.Middleware(s.verifier, s))
		r.Get("/flows", s.hub.ServeWS("flows"))
		r.Get("/events", s.hub.ServeWS("events"))
		r.Get("/alerts", s.hub.ServeWS("alerts"))
		r.Get("/logs", s.streamLogs)
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
	// Self-protection: a policy authored here may not target the console.
	if err := s.guard.CheckManifest(body); err != nil {
		s.record(req, "policy.apply", string(kind)+" "+ns+"/"+name, audit.OutcomeDenied, err.Error(), nil, nil)
		writeErr(w, http.StatusForbidden, err)
		return
	}
	if exempted, changed, err := guard.Exempt(body); err == nil && changed {
		body = exempted
	}
	before, _ := s.policies.Get(req.Context(), kind, ns, name)
	// GitOps PR mode: open a PR instead of applying live.
	if req.URL.Query().Get("mode") == "pr" {
		s.applyViaPR(w, req, kind, name, body, "apply "+string(kind)+"/"+name)
		return
	}
	p, err := s.policies.Apply(req.Context(), kind, ns, name, body)
	if err != nil {
		s.record(req, "policy.apply", string(kind)+" "+ns+"/"+name, audit.OutcomeError, err.Error(), nil, nil)
		writeErr(w, statusOf(err), err)
		return
	}
	var beforeManifest json.RawMessage
	if before != nil {
		beforeManifest = before.Manifest
	}
	s.record(req, "policy.apply", string(kind)+" "+ns+"/"+name, audit.OutcomeSuccess, "", beforeManifest, p.Manifest)
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
	kind := resolveKind(kindsList, ns)
	name := chi.URLParam(req, "name")
	before, _ := s.policies.Get(req.Context(), kind, ns, name)
	if err := s.policies.Delete(req.Context(), kind, ns, name); err != nil {
		s.record(req, "policy.delete", string(kind)+" "+ns+"/"+name, audit.OutcomeError, err.Error(), nil, nil)
		writeErr(w, statusOf(err), err)
		return
	}
	var beforeManifest json.RawMessage
	if before != nil {
		beforeManifest = before.Manifest
	}
	s.record(req, "policy.delete", string(kind)+" "+ns+"/"+name, audit.OutcomeSuccess, "", beforeManifest, nil)
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
