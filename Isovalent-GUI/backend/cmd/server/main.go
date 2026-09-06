// Command server runs the isovalent-control API.
//
// There is exactly one data path: a real cluster. Hubble Relay, Tetragon and
// the Kubernetes API are all required. If one of them is unreachable the
// server still starts — a console that refuses to boot when Tetragon is down
// cannot tell you that Tetragon is down — but it says so loudly, and the
// Diagnostics page shows exactly which leg is broken.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/ai"
	"github.com/isovalent-control/isovalent-control/backend/internal/alerts"
	"github.com/isovalent-control/isovalent-control/backend/internal/apikeys"
	"github.com/isovalent-control/isovalent-control/backend/internal/audit"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/config"
	"github.com/isovalent-control/isovalent-control/backend/internal/gitops"
	"github.com/isovalent-control/isovalent-control/backend/internal/guard"
	"github.com/isovalent-control/isovalent-control/backend/internal/hits"
	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/logbuf"
	"github.com/isovalent-control/isovalent-control/backend/internal/proxy"
	"github.com/isovalent-control/isovalent-control/backend/internal/server"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/stream"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
	"github.com/isovalent-control/isovalent-control/backend/internal/version"
)

func main() {
	// `-version` so a deploy script can prove which build is in the image
	// without needing a shell in a distroless container.
	showVersion := flag.Bool("version", false, "print the build version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return
	}

	cfg := config.Load()

	// Every log line is also kept in a ring the console can read back, so
	// "why is this table empty?" is answerable from the UI.
	logs := logbuf.New(4000)
	slog.SetDefault(slog.New(logbuf.NewHandler(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}), logs)))
	slog.Info("isovalent-control starting",
		"version", version.Version, "commit", version.Commit, "cluster", cfg.ClusterName)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	k8sClient, err := k8s.NewClient(k8s.Options{
		APIServer: cfg.K8sAPIServer,
		Token:     cfg.K8sToken,
		TokenFile: cfg.K8sTokenFile,
		CAFile:    cfg.K8sCAFile,
		Insecure:  cfg.K8sInsecure,
	})
	if err != nil {
		slog.Error("kubernetes client: the console cannot manage policy without API access", "err", err)
		os.Exit(1)
	}
	policies := k8s.NewLiveStore(k8sClient)

	flowSrc := hubble.NewLiveSource(cfg.HubbleRelayAddr)
	eventSrc := tetragon.NewLiveSource(cfg.TetragonAddr)
	slog.Info("data sources", "hubble", cfg.HubbleRelayAddr, "tetragon", cfg.TetragonAddr)

	// A dial failure here is not fatal: the stream reconnects with backoff,
	// and the console is more useful reporting the outage than exiting.
	flows, err := flowSrc.Flows(ctx)
	if err != nil {
		slog.Error("hubble relay unreachable; flow data will be empty until it recovers", "err", err)
		flows = closedFlows()
	}
	events, err := eventSrc.Events(ctx)
	if err != nil {
		slog.Error("tetragon unreachable; runtime data will be empty until it recovers", "err", err)
		events = closedEvents()
	}

	hub := stream.NewHub(cfg.CORSOrigin)
	logs.SetPublisher(func(l logbuf.Line) { hub.Publish("logs", l) })
	agg := server.NewAggregator(hub)

	// Historical store: Postgres when IC_DB_DSN is set, else in-memory ring.
	var hist store.Store = store.NewMemoryStore(cfg.HistoryLimit)
	auditLog := audit.New(5000)
	if cfg.DBDSN != "" {
		if pg, err := store.NewPostgresStore(ctx, cfg.DBDSN); err != nil {
			slog.Warn("postgres unavailable; falling back to in-memory history", "err", err)
		} else {
			hist = pg
			slog.Info("historical store: postgres")
			if sink, err := audit.NewPostgresSink(ctx, pg); err != nil {
				slog.Warn("audit table unavailable; audit log is in-memory only", "err", err)
			} else {
				auditLog.SetSink(sink)
			}
		}
	} else {
		slog.Info("historical store: in-memory ring", "recordsPerKind", cfg.HistoryLimit)
	}
	agg.SetStore(hist)

	tracker := hits.New()
	agg.SetHits(tracker)

	router := alerts.NewRouter()
	agg.SetRouter(router)

	gh := gitops.New(cfg.GitHubRepo, cfg.GitHubToken, cfg.GitHubBase, cfg.GitHubPath, cfg.GitHubAPIURL)
	if gh.Enabled() {
		slog.Info("gitops PR mode enabled", "repo", cfg.GitHubRepo)
	}

	keys := apikeys.New()
	if n := keys.LoadStatic(cfg.APITokens); n > 0 {
		slog.Info("static API tokens loaded", "count", n)
	}

	g := guard.New(cfg.ProtectedNamespaces)
	slog.Info("self-protection active", "protectedNamespaces", g.Protected())

	assistant := ai.New(ai.Config{
		Provider: ai.Provider(cfg.AIProvider),
		BaseURL:  cfg.AIBaseURL,
		Model:    cfg.AIModel,
		APIKey:   cfg.AIAPIKey,
	})
	if st := assistant.Status(); st.Enabled {
		slog.Info("ai assistant enabled", "provider", st.Provider, "model", st.Model)
	} else {
		slog.Info("ai assistant disabled", "reason", st.Reason)
	}

	hubbleUI, err := proxy.New("hubble-ui", "/hubble-ui", cfg.HubbleUIURL)
	if err != nil {
		slog.Warn("hubble UI embed unavailable", "err", err)
	}
	grafana, err := proxy.New("grafana", "/grafana", cfg.GrafanaURL)
	if err != nil {
		slog.Warn("grafana embed unavailable", "err", err)
	}

	go agg.Run(ctx, flows, events)

	var verifier *auth.Verifier
	if cfg.OIDCIssuer != "" {
		verifier = &auth.Verifier{Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, RolesClaim: cfg.OIDCRolesClaim}
		slog.Info("oidc enabled", "issuer", cfg.OIDCIssuer)
	} else {
		slog.Warn("authentication DISABLED (dev mode) — set IC_OIDC_ISSUER before exposing this beyond a lab")
	}

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: server.New(cfg, hub, agg, policies, verifier, server.Deps{
			Router: router, Store: hist, GitOps: gh, K8s: k8sClient,
			Audit: auditLog, Logs: logs, Hits: tracker, Guard: g,
			AI: assistant, Keys: keys, HubbleUI: hubbleUI, Grafana: grafana,
		}).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("listening", "addr", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func closedFlows() <-chan hubble.Flow {
	ch := make(chan hubble.Flow)
	close(ch)
	return ch
}

func closedEvents() <-chan tetragon.Event {
	ch := make(chan tetragon.Event)
	close(ch)
	return ch
}
