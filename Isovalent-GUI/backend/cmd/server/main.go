// Command server runs the isovalent-control API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/alerts"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/config"
	"github.com/isovalent-control/isovalent-control/backend/internal/gitops"
	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/mock"
	"github.com/isovalent-control/isovalent-control/backend/internal/server"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/stream"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		flowSrc  hubble.Source
		eventSrc tetragon.Source
		policies k8s.PolicyStore
	)
	switch cfg.Mode {
	case config.ModeLive:
		flowSrc = hubble.NewLiveSource(cfg.HubbleRelayAddr)
		eventSrc = tetragon.NewLiveSource(cfg.TetragonAddr)
		client, err := k8s.NewClient(k8s.Options{
			APIServer: cfg.K8sAPIServer,
			Token:     cfg.K8sToken,
			TokenFile: cfg.K8sTokenFile,
			CAFile:    cfg.K8sCAFile,
			Insecure:  cfg.K8sInsecure,
		})
		if err != nil {
			slog.Error("kubernetes client", "err", err)
			os.Exit(1)
		}
		policies = k8s.NewLiveStore(client)
		slog.Info("live mode", "hubble", cfg.HubbleRelayAddr, "tetragon", cfg.TetragonAddr)
	default:
		flowSrc = mock.NewHubbleSource()
		eventSrc = mock.NewTetragonSource()
		policies = k8s.NewMockStore()
		slog.Info("mock mode: serving generated demo data (set IC_MODE=live for a real cluster)")
	}

	flows, err := flowSrc.Flows(ctx)
	if err != nil {
		slog.Error("flow source", "err", err)
		os.Exit(1)
	}
	events, err := eventSrc.Events(ctx)
	if err != nil {
		slog.Error("event source", "err", err)
		os.Exit(1)
	}

	hub := stream.NewHub(cfg.CORSOrigin)
	agg := server.NewAggregator(hub)

	// Historical store: Postgres when IC_DB_DSN is set, else in-memory ring
	// bounded to the retention window (default 14 days).
	mem := store.NewMemoryStore(200000)
	mem.SetMaxAge(time.Duration(cfg.RetentionDays) * 24 * time.Hour)
	var hist store.Store = mem
	if cfg.DBDSN != "" {
		if pg, err := store.NewPostgresStore(ctx, cfg.DBDSN); err != nil {
			slog.Warn("postgres unavailable; falling back to in-memory store", "err", err)
		} else {
			hist = pg
			slog.Info("historical store: postgres", "retentionDays", cfg.RetentionDays)
		}
	}
	agg.SetStore(hist)

	// Alert router (external notification sinks configured via the API).
	router := alerts.NewRouter()
	agg.SetRouter(router)

	// GitOps PR apply mode (optional).
	gh := gitops.New(cfg.GitHubRepo, cfg.GitHubToken, cfg.GitHubBase, cfg.GitHubPath, cfg.GitHubAPIURL)
	if gh.Enabled() {
		slog.Info("gitops PR mode enabled", "repo", cfg.GitHubRepo)
	}

	go agg.Run(ctx, flows, events)

	var verifier *auth.Verifier
	if cfg.OIDCIssuer != "" {
		verifier = &auth.Verifier{Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, RolesClaim: cfg.OIDCRolesClaim}
		slog.Info("oidc enabled", "issuer", cfg.OIDCIssuer)
	} else {
		slog.Warn("authentication DISABLED (dev mode) — set IC_OIDC_ISSUER in production")
	}

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: server.New(cfg, hub, agg, policies, verifier, server.Deps{
			Router: router, Store: hist, GitOps: gh,
		}).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("listening", "addr", cfg.ListenAddr, "mode", cfg.Mode)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
