// Package config loads server configuration from the environment.
//
// Everything the console shows comes from a real cluster. There is no
// generated-data path anywhere in the server: a console that can invent a flow
// is one you cannot trust when it tells you a flow was dropped.
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config is the full server configuration.
type Config struct {
	ListenAddr string
	CORSOrigin string

	// Data plane endpoints.
	HubbleRelayAddr string
	TetragonAddr    string

	// Kubernetes API access. If APIServer is empty the client attempts
	// in-cluster configuration, then falls back to http://127.0.0.1:8001
	// (kubectl proxy).
	K8sAPIServer string
	K8sToken     string
	K8sTokenFile string
	K8sCAFile    string
	K8sInsecure  bool
	ClusterName  string

	// SelfNamespace is the namespace the console itself runs in. Policies
	// authored here can never select it — see internal/guard.
	SelfNamespace string
	// ProtectedNamespaces are additionally never selectable by authored
	// policies (comma-separated). SelfNamespace is always included.
	ProtectedNamespaces []string

	// Embedded consoles, reverse-proxied so they can be framed.
	HubbleUIURL string
	GrafanaURL  string
	// GrafanaDashboardUID pre-selects the shipped dashboard in the embed.
	GrafanaDashboardUID string

	// OIDC. Empty issuer disables authentication (dev mode).
	OIDCIssuer   string
	OIDCClientID string
	// Claim holding role assignments (default "groups").
	OIDCRolesClaim string

	// Static API tokens for machine clients, "name:token" comma-separated.
	// Tokens created in the UI are additional to these.
	APITokens string

	// Historical store. Empty DSN keeps everything in an in-memory ring.
	DBDSN string
	// HistoryLimit is the in-memory ring size per record kind.
	HistoryLimit int

	// External AI assistant.
	AIProvider   string // anthropic | gemini | openai | disabled
	AIBaseURL    string // override for OpenAI-compatible gateways
	AIModel      string
	AIAPIKey     string
	AIAPIKeyFile string

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
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func csv(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads configuration from IC_* environment variables.
func Load() Config {
	insecure, _ := strconv.ParseBool(env("IC_K8S_INSECURE", "false"))
	self := env("IC_SELF_NAMESPACE", "isovalent-control")
	protected := csv(env("IC_PROTECTED_NAMESPACES", "kube-system"))
	protected = append(protected, self)

	key := os.Getenv("IC_AI_API_KEY")
	keyFile := os.Getenv("IC_AI_API_KEY_FILE")
	if key == "" && keyFile != "" {
		if b, err := os.ReadFile(keyFile); err == nil {
			key = strings.TrimSpace(string(b))
		}
	}

	return Config{
		ListenAddr:      env("IC_LISTEN_ADDR", ":8081"),
		CORSOrigin:      env("IC_CORS_ORIGIN", "*"),
		HubbleRelayAddr: env("IC_HUBBLE_RELAY_ADDR", "localhost:4245"),
		TetragonAddr:    env("IC_TETRAGON_ADDR", "localhost:54321"),

		K8sAPIServer: os.Getenv("IC_K8S_API_SERVER"),
		K8sToken:     os.Getenv("IC_K8S_TOKEN"),
		K8sTokenFile: os.Getenv("IC_K8S_TOKEN_FILE"),
		K8sCAFile:    os.Getenv("IC_K8S_CA_FILE"),
		K8sInsecure:  insecure,
		ClusterName:  env("IC_CLUSTER_NAME", "current-context"),

		SelfNamespace:       self,
		ProtectedNamespaces: protected,

		HubbleUIURL:         env("IC_HUBBLE_UI_URL", "http://hubble-ui.kube-system.svc.cluster.local:80"),
		GrafanaURL:          env("IC_GRAFANA_URL", "http://kube-prometheus-stack-grafana.monitoring.svc.cluster.local:80"),
		GrafanaDashboardUID: env("IC_GRAFANA_DASHBOARD_UID", "isovalent-control"),

		OIDCIssuer:     os.Getenv("IC_OIDC_ISSUER"),
		OIDCClientID:   os.Getenv("IC_OIDC_CLIENT_ID"),
		OIDCRolesClaim: env("IC_OIDC_ROLES_CLAIM", "groups"),
		APITokens:      os.Getenv("IC_API_TOKENS"),

		DBDSN:        os.Getenv("IC_DB_DSN"),
		HistoryLimit: envInt("IC_HISTORY_LIMIT", 20000),

		AIProvider:   env("IC_AI_PROVIDER", "disabled"),
		AIBaseURL:    os.Getenv("IC_AI_BASE_URL"),
		AIModel:      os.Getenv("IC_AI_MODEL"),
		AIAPIKey:     key,
		AIAPIKeyFile: keyFile,

		GitHubRepo:   os.Getenv("IC_GITHUB_REPO"),
		GitHubToken:  os.Getenv("IC_GITHUB_TOKEN"),
		GitHubBase:   env("IC_GITHUB_BASE", "main"),
		GitHubPath:   env("IC_GITHUB_PATH", "policies"),
		GitHubAPIURL: env("IC_GITHUB_API_URL", "https://api.github.com"),
	}
}
