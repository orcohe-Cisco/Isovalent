// Package config loads server configuration from the environment.
package config

import (
	"os"
	"strconv"
)

// Mode selects where flow/event/policy data comes from.
type Mode string

const (
	// ModeMock serves generated demo data — no cluster required.
	ModeMock Mode = "mock"
	// ModeLive connects to Hubble Relay, Tetragon and the Kubernetes API.
	ModeLive Mode = "live"
)

// Config is the full server configuration.
type Config struct {
	Mode       Mode
	ListenAddr string
	CORSOrigin string

	// Live-mode endpoints.
	HubbleRelayAddr string
	TetragonAddr    string

	// Kubernetes API access (live mode). If APIServer is empty the client
	// attempts in-cluster configuration, then falls back to
	// http://127.0.0.1:8001 (kubectl proxy).
	K8sAPIServer string
	K8sToken     string
	K8sTokenFile string
	K8sCAFile    string
	K8sInsecure  bool
	ClusterName  string

	// OIDC. Empty issuer disables authentication (dev mode).
	OIDCIssuer   string
	OIDCClientID string
	// Claim holding role assignments (default "groups").
	OIDCRolesClaim string

	// Historical store. Empty DSN keeps everything in an in-memory ring.
	DBDSN string
	// Retention window for the enforcement/event log (days).
	RetentionDays int

	// External UIs embedded in the app (the original Hubble UI + Grafana).
	HubbleUIURL string
	GrafanaURL  string
	// Dashboard opened by default in the embedded Grafana. Empty embeds
	// Grafana's home page instead.
	GrafanaDashboardUID string

	// GitOps PR apply mode. When GitHubRepo + GitHubToken are set, policy
	// applies can render to a branch and open a PR instead of applying live.
	GitHubRepo   string // "owner/repo"
	GitHubToken  string
	GitHubBase   string // base branch, default "main"
	GitHubPath   string // directory in the repo for rendered policies
	GitHubAPIURL string // default https://api.github.com
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Load reads configuration from IC_* environment variables.
func Load() Config {
	insecure, _ := strconv.ParseBool(env("IC_K8S_INSECURE", "false"))
	return Config{
		Mode:                Mode(env("IC_MODE", string(ModeMock))),
		ListenAddr:          env("IC_LISTEN_ADDR", ":8081"),
		CORSOrigin:          env("IC_CORS_ORIGIN", "*"),
		HubbleRelayAddr:     env("IC_HUBBLE_RELAY_ADDR", "localhost:4245"),
		TetragonAddr:        env("IC_TETRAGON_ADDR", "localhost:54321"),
		K8sAPIServer:        os.Getenv("IC_K8S_API_SERVER"),
		K8sToken:            os.Getenv("IC_K8S_TOKEN"),
		K8sTokenFile:        os.Getenv("IC_K8S_TOKEN_FILE"),
		K8sCAFile:           os.Getenv("IC_K8S_CA_FILE"),
		K8sInsecure:         insecure,
		ClusterName:         env("IC_CLUSTER_NAME", "default"),
		OIDCIssuer:          os.Getenv("IC_OIDC_ISSUER"),
		OIDCClientID:        os.Getenv("IC_OIDC_CLIENT_ID"),
		OIDCRolesClaim:      env("IC_OIDC_ROLES_CLAIM", "groups"),
		DBDSN:               os.Getenv("IC_DB_DSN"),
		RetentionDays:       envInt("IC_RETENTION_DAYS", 14),
		HubbleUIURL:         os.Getenv("IC_HUBBLE_UI_URL"),
		GrafanaURL:          os.Getenv("IC_GRAFANA_URL"),
		GrafanaDashboardUID: env("IC_GRAFANA_DASHBOARD_UID", "isovalent-control"),
		GitHubRepo:          os.Getenv("IC_GITHUB_REPO"),
		GitHubToken:         os.Getenv("IC_GITHUB_TOKEN"),
		GitHubBase:          env("IC_GITHUB_BASE", "main"),
		GitHubPath:          env("IC_GITHUB_PATH", "policies"),
		GitHubAPIURL:        env("IC_GITHUB_API_URL", "https://api.github.com"),
	}
}
