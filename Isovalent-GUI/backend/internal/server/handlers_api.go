package server

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/isovalent-control/isovalent-control/backend/internal/audit"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
)

//go:embed openapi.yaml
var openAPISpec []byte

// getOpenAPI serves the API contract. It is unauthenticated on purpose: you
// need to read the docs before you have a token.
func (s *Server) getOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openAPISpec)
}

// --- API tokens ----------------------------------------------------------

func (s *Server) listAPIKeys(w http.ResponseWriter, req *http.Request) {
	if !auth.FromContext(req.Context()).IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("API tokens require the admin role"))
		return
	}
	if s.keys == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.keys.List())
}

func (s *Server) createAPIKey(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	if !id.IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("API tokens require the admin role"))
		return
	}
	var body struct {
		Name    string `json:"name"`
		Role    string `json:"role"`
		TTLDays int    `json:"ttlDays"`
	}
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Role == "" {
		body.Role = "viewer"
	}
	var ttl time.Duration
	if body.TTLDays > 0 {
		ttl = time.Duration(body.TTLDays) * 24 * time.Hour
	}
	key, plaintext, err := s.keys.Create(body.Name, body.Role, id.Subject, ttl)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.record(req, "apikey.create", body.Name+" ("+body.Role+")", audit.OutcomeSuccess, "", nil, nil)
	// The plaintext appears here and nowhere else, ever.
	writeJSON(w, http.StatusCreated, map[string]any{"key": key, "token": plaintext})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, req *http.Request) {
	if !auth.FromContext(req.Context()).IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("API tokens require the admin role"))
		return
	}
	name, err := s.keys.Revoke(chi.URLParam(req, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.record(req, "apikey.revoke", name, audit.OutcomeSuccess, "", nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- deployment command catalogue ---------------------------------------

// DeployStep is one copyable command block in the Deployment tab.
type DeployStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Commands are keyed by OS ("macos", "linux", "windows") when they differ,
	// and by "all" when they do not.
	Commands map[string]string `json:"commands"`
	Note     string            `json:"note,omitempty"`
	Optional bool              `json:"optional,omitempty"`
}

// DeployPlatform is one target environment.
type DeployPlatform struct {
	ID    string       `json:"id"`
	Label string       `json:"label"`
	Note  string       `json:"note,omitempty"`
	Steps []DeployStep `json:"steps"`
}

// getDeployment returns everything needed to stand this up on another cluster.
//
// It is generated rather than written in the docs so it always reflects the
// running console's own address and namespace — a deployment guide that tells
// you to connect to the wrong port is worse than none.
func (s *Server) getDeployment(w http.ResponseWriter, _ *http.Request) {
	ns := s.cfg.SelfNamespace
	writeJSON(w, http.StatusOK, map[string]any{
		"cluster":    s.cfg.ClusterName,
		"namespace":  ns,
		"oses":       []map[string]string{{"id": "macos", "label": "macOS"}, {"id": "linux", "label": "Linux"}, {"id": "windows", "label": "Windows (PowerShell)"}},
		"platforms":  deployPlatforms(ns, s.cfg.HubbleRelayAddr, s.cfg.TetragonAddr),
		"connectEnv": connectEnv(ns),
	})
}

func connectEnv(ns string) []map[string]string {
	return []map[string]string{
		{"name": "IC_HUBBLE_RELAY_ADDR", "value": "hubble-relay.kube-system.svc.cluster.local:80", "note": "Hubble Relay gRPC. Use localhost:4245 when port-forwarding."},
		{"name": "IC_TETRAGON_ADDR", "value": "tetragon.kube-system.svc.cluster.local:54321", "note": "Tetragon gRPC. Use localhost:54321 when port-forwarding."},
		{"name": "IC_HUBBLE_UI_URL", "value": "http://hubble-ui.kube-system.svc.cluster.local:80", "note": "Embedded service map."},
		{"name": "IC_GRAFANA_URL", "value": "http://kube-prometheus-stack-grafana.monitoring.svc.cluster.local:80", "note": "Embedded dashboards."},
		{"name": "IC_CLUSTER_NAME", "value": "my-cluster", "note": "Shown in the sidebar and in every alert."},
		{"name": "IC_SELF_NAMESPACE", "value": ns, "note": "Never selectable by policies authored in the console."},
		{"name": "IC_DB_DSN", "value": "postgres://ic:ic@postgres:5432/ic?sslmode=disable", "note": "Optional. Without it, history is an in-memory ring lost on restart."},
		{"name": "IC_OIDC_ISSUER", "value": "https://issuer.example.com", "note": "Required before exposing the console beyond a lab."},
		{"name": "IC_AI_PROVIDER", "value": "anthropic | gemini | openai", "note": "With IC_AI_API_KEY_FILE pointing at a mounted Secret."},
	}
}

func deployPlatforms(ns, relay, tetra string) []DeployPlatform {
	// The kubectl/helm commands themselves are identical everywhere; only the
	// tool installation and the shell differ. Rather than pretend otherwise,
	// each step carries per-OS text only where it genuinely differs.
	cilium := `helm repo add cilium https://helm.cilium.io
helm repo update
helm upgrade --install cilium cilium/cilium --version 1.16.5 \
  --namespace kube-system \
  --set hubble.enabled=true \
  --set hubble.relay.enabled=true \
  --set hubble.ui.enabled=true \
  --set hubble.metrics.enableOpenMetrics=true \
  --set hubble.metrics.enabled="{dns,drop,tcp,flow,port-distribution,icmp,httpV2:exemplars=true;labelsContext=source_ip\,source_namespace\,source_workload\,destination_ip\,destination_namespace\,destination_workload\,traffic_direction}"
kubectl -n kube-system rollout status deploy/hubble-relay`

	tetragon := `helm repo add cilium https://helm.cilium.io
helm repo update
helm upgrade --install tetragon cilium/tetragon \
  --namespace kube-system \
  --set tetragon.grpc.enabled=true \
  --set tetragon.grpc.address="localhost:54321" \
  --set tetragon.exportAllowList="" \
  --set tetragon.enableProcessCred=true \
  --set tetragon.enableProcessNs=true
kubectl -n kube-system rollout status ds/tetragon`

	connect := `# Point the console at this cluster's data plane
kubectl -n ` + ns + ` set env deploy/isovalent-control-backend \
  IC_HUBBLE_RELAY_ADDR=` + relay + ` \
  IC_TETRAGON_ADDR=` + tetra + ` \
  IC_CLUSTER_NAME="$(kubectl config current-context)"
kubectl -n ` + ns + ` rollout status deploy/isovalent-control-backend`

	install := `# From a checkout of this repository
./run.sh                 # everything: agents, console, GOAT, Grafana, port-forwards
./run.sh --no-goat       # skip Kubernetes GOAT
./run.sh --no-grafana    # skip the Prometheus/Grafana stack
./run.sh --registry ghcr.io/you   # push images somewhere the cluster can pull from`

	base := []DeployStep{
		{ID: "context", Title: "Select the cluster", Body: "Everything below applies to whatever kubectl is pointed at. Check that first — this is the step people skip and then debug for an hour.",
			Commands: map[string]string{
				"all":     "kubectl config get-contexts\nkubectl config use-context <name>\nkubectl cluster-info",
				"windows": "kubectl config get-contexts\nkubectl config use-context <name>\nkubectl cluster-info",
			}},
		{ID: "cilium", Title: "Install Cilium with Hubble", Body: "Hubble Relay provides the flow stream; Hubble UI is what the console embeds as the service map.",
			Commands: map[string]string{"all": cilium},
			Note:     "If Cilium is already the CNI, this upgrades it in place and only turns Hubble on."},
		{ID: "tetragon", Title: "Install Tetragon", Body: "The runtime-security event source. enableProcessCred is what makes the user column in the Exclusions tab populated rather than empty.",
			Commands: map[string]string{"all": tetragon}},
		{ID: "console", Title: "Deploy Isovalent Control", Body: "One script, no cloud-specific arguments.",
			Commands: map[string]string{"all": install}},
		{ID: "connect", Title: "Connect the console to this cluster", Body: "Only needed if the console runs outside the cluster it observes.",
			Commands: map[string]string{"all": connect}, Optional: true},
	}

	return []DeployPlatform{
		{ID: "generic", Label: "Any Kubernetes", Note: "Works anywhere kubectl and helm do.", Steps: base},
		{ID: "aks", Label: "Azure AKS", Note: "AKS ships its own CNI; Cilium can run in overlay mode alongside it or replace it. Bring your own cluster — the console does not create one.",
			Steps: append([]DeployStep{{
				ID: "credentials", Title: "Get cluster credentials",
				Body:     "Fill in your own resource group and cluster name.",
				Commands: map[string]string{"all": "az aks get-credentials --resource-group <resource-group> --name <cluster-name>"},
			}}, base...)},
		{ID: "eks", Label: "Amazon EKS", Note: "Cilium replaces the VPC CNI in kube-proxy-replacement mode; check the Cilium EKS guide before changing an existing cluster.",
			Steps: append([]DeployStep{{
				ID: "credentials", Title: "Get cluster credentials",
				Commands: map[string]string{"all": "aws eks update-kubeconfig --region <region> --name <cluster-name>"},
			}}, base...)},
		{ID: "gke", Label: "Google GKE", Note: "Use a Dataplane V2 cluster, or a standard cluster with the default CNI removed.",
			Steps: append([]DeployStep{{
				ID: "credentials", Title: "Get cluster credentials",
				Commands: map[string]string{"all": "gcloud container clusters get-credentials <cluster-name> --region <region>"},
			}}, base...)},
		{ID: "kind", Label: "Local (kind / k3d / minikube)", Note: "Tetragon needs a kernel with BTF. kind on Docker Desktop for macOS works; the Sigkill enforcement paths need BPF-LSM, which that kernel does not always have.",
			Steps: append([]DeployStep{{
				ID: "cluster", Title: "Create a local cluster",
				Commands: map[string]string{
					"all":     "kind create cluster --name isovalent --config deploy/local/kind.yaml",
					"windows": "kind create cluster --name isovalent --config deploy\\local\\kind.yaml",
				},
			}}, base...)},
	}
}
