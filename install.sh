#!/usr/bin/env bash
#
#  isovalent-control — universal installer
#  ---------------------------------------
#  Works on ANY Kubernetes: kind, minikube, k3s, Docker Desktop, EKS, AKS, GKE.
#  By default it installs in DEMO (mock) mode — no Cilium/Hubble/Tetragon needed,
#  so it runs anywhere and shows the full UI immediately.
#
#  Quick start (any cluster):   ./install.sh
#  Local cluster, build locally: ./install.sh --build
#  Real observability (needs Cilium+Hubble+Tetragon): ./install.sh --live
#  Everything:  ./install.sh --live --install-stack --with-monitoring
#
#  Kubernetes Goat is NOT installed by default. Add --with-goat to pull it from
#  its own GitHub into a separate folder and deploy it as an intentionally
#  vulnerable demo target.
#
#  Run  ./install.sh --help  for all options.
set -euo pipefail

# ------------------------------------------------------------------ defaults
# Booleans accept true/yes/1/on (case-insensitive) — see truthy() below.
NS="${NS:-isovalent-control}"
MODE="${MODE:-live}"                          # mock | live
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/orcohe-cisco}"
IMAGE_TAG="${IMAGE_TAG:-0.5.0}"
ACR="${ACR:-}"                                # Azure Container Registry name (AKS)
BUILD="${BUILD:-false}"                       # build images locally + load
INSTALL_STACK="${INSTALL_STACK:-true}"        # install Cilium+Hubble+Tetragon
WITH_MONITORING="${WITH_MONITORING:-true}"    # Prometheus + Grafana
WITH_GOAT="${WITH_GOAT:-true}"                # Kubernetes Goat (vulnerable demo)
GOAT_DIR="${GOAT_DIR:-./_goat}"               # separate folder, never mixed in
PORT_FORWARD="${PORT_FORWARD:-true}"
DO_UNINSTALL=false
FE_PORT=3000
BE_PORT=8081

# Accept true/yes/y/1/on so hand-edited settings don't silently no-op.
truthy() {
  case "$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')" in
    true|yes|y|1|on) return 0 ;;
    *) return 1 ;;
  esac
}

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
MANIFEST="$REPO_ROOT/deploy/manifests/isovalent-control.yaml"

c()   { printf '\033[1;36m%s\033[0m\n' "$*"; }   # cyan heading
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
warn(){ printf '\033[1;33m!\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

usage() {
  cat <<EOF
isovalent-control — universal installer

Works on ANY Kubernetes: kind, minikube, k3s, Docker Desktop, EKS, AKS, GKE.

Defaults install the FULL setup: live mode + Cilium/Hubble/Tetragon +
Prometheus/Grafana + Kubernetes Goat. Turn pieces off with --no-* flags.

  ./install.sh                        full setup (public images)
  ./install.sh --acr myregistry       AKS: build in ACR + wire pull secret
  ./install.sh --build                kind/minikube: build locally, no registry
  ./install.sh --mock --no-stack --no-goat --no-monitoring   minimal demo

Options:
  --live / --mock        real data vs demo data           (default: $MODE)
  --install-stack / --no-stack        Cilium+Hubble+Tetragon  (default: $INSTALL_STACK)
  --with-monitoring / --no-monitoring Prometheus+Grafana      (default: $WITH_MONITORING)
  --with-goat / --no-goat             Kubernetes Goat         (default: $WITH_GOAT)
  --acr NAME             build images in Azure Container Registry + pull secret
  --build                build images locally and load them (kind/minikube)
  --image-repo REPO      registry for images (default: $IMAGE_REPO)
  --tag TAG              image tag (default: $IMAGE_TAG)
  --namespace NS         install namespace (default: $NS)
  --no-port-forward      don't open port-forwards at the end
  --uninstall            remove isovalent-control
  --help

Booleans in the settings block accept true/yes/1/on.
Kubernetes Goat is intentionally vulnerable — it is cloned from its own
GitHub into $GOAT_DIR and is easy to skip with --no-goat.
EOF
}

# ------------------------------------------------------------------ args
while [ $# -gt 0 ]; do
  case "$1" in
    --live) MODE="live" ;;
    --mock|--demo) MODE="mock" ;;
    --install-stack) INSTALL_STACK=true ;;
    --no-stack) INSTALL_STACK=false ;;
    --with-monitoring) WITH_MONITORING=true ;;
    --no-monitoring) WITH_MONITORING=false ;;
    --with-goat) WITH_GOAT=true ;;
    --no-goat) WITH_GOAT=false ;;
    --build) BUILD=true ;;
    --acr) ACR="$2"; shift ;;
    --image-repo) IMAGE_REPO="$2"; shift ;;
    --tag) IMAGE_TAG="$2"; shift ;;
    --namespace) NS="$2"; shift ;;
    --no-port-forward) PORT_FORWARD=false ;;
    --uninstall) DO_UNINSTALL=true ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
  shift
done

# Normalize booleans once so every later test is consistent.
truthy "$BUILD"           && BUILD=true           || BUILD=false
truthy "$INSTALL_STACK"   && INSTALL_STACK=true   || INSTALL_STACK=false
truthy "$WITH_MONITORING" && WITH_MONITORING=true || WITH_MONITORING=false
truthy "$WITH_GOAT"       && WITH_GOAT=true       || WITH_GOAT=false
truthy "$PORT_FORWARD"    && PORT_FORWARD=true    || PORT_FORWARD=false

# ------------------------------------------------------------------ preflight
command -v kubectl >/dev/null || die "kubectl not found — install it first."
command -v envsubst >/dev/null || die "envsubst not found (install gettext: brew install gettext / apt-get install gettext-base)."
[ -f "$MANIFEST" ] || die "manifest not found at $MANIFEST — run from the repo root."
kubectl cluster-info >/dev/null 2>&1 || die "kubectl can't reach a cluster. Point your kube-context at one and retry."

CTX="$(kubectl config current-context 2>/dev/null || echo unknown)"
NODE0="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}{" "}{.items[0].spec.providerID}' 2>/dev/null || true)"
PLATFORM="generic"
case "$NODE0" in
  *kind*)       PLATFORM="kind" ;;
  *minikube*)   PLATFORM="minikube" ;;
  *aws*|*eks*)  PLATFORM="eks" ;;
  *azure*|*aks*)PLATFORM="aks" ;;
  *gce*|*gke*)  PLATFORM="gke" ;;
  *k3s*)        PLATFORM="k3s" ;;
esac

if [ "$DO_UNINSTALL" = true ]; then
  c "Uninstalling isovalent-control from context '$CTX'"
  kubectl delete namespace "$NS" --ignore-not-found
  kubectl delete clusterrole,clusterrolebinding isovalent-control-policies --ignore-not-found
  ok "Removed. (Cilium/Tetragon/monitoring/Goat, if installed, were left in place.)"
  exit 0
fi

c "isovalent-control installer"
echo "  context:  $CTX"
echo "  platform: $PLATFORM"
echo "  mode:     $MODE"
echo "  images:   $IMAGE_REPO/isovalent-control-{backend,frontend}:$IMAGE_TAG"
echo "  namespace:$NS   monitoring:$WITH_MONITORING   goat:$WITH_GOAT"
echo

# ------------------------------------------------------------------ images
export IC_IMAGE_TAG="$IMAGE_TAG"
if [ -n "$ACR" ]; then
  # AKS path: build server-side in Azure Container Registry (no local Docker)
  # and wire an image-pull secret from the ACR admin credentials — no
  # subscription role assignment needed.
  command -v az >/dev/null || die "--acr needs the az CLI."
  c "Building images in ACR '$ACR' (server-side, no local Docker)"
  az acr build -r "$ACR" -t "isovalent-control-backend:$IMAGE_TAG"  "$REPO_ROOT/backend"
  az acr build -r "$ACR" -t "isovalent-control-frontend:$IMAGE_TAG" "$REPO_ROOT/frontend"
  az acr update -n "$ACR" --admin-enabled true >/dev/null
  ACR_USER=$(az acr credential show -n "$ACR" --query username -o tsv)
  ACR_PASS=$(az acr credential show -n "$ACR" --query 'passwords[0].value' -o tsv)
  kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
  kubectl -n "$NS" create secret docker-registry acr-pull \
    --docker-server="$ACR.azurecr.io" --docker-username="$ACR_USER" --docker-password="$ACR_PASS" \
    --dry-run=client -o yaml | kubectl apply -f -
  export IC_IMAGE_PREFIX="$ACR.azurecr.io/"
  NEEDS_PULL_SECRET=true
  ok "Images in $ACR.azurecr.io, pull secret wired"
elif [ "$BUILD" = true ]; then
  command -v docker >/dev/null || die "--build needs docker."
  c "Building images locally"
  docker build -t "isovalent-control-backend:$IMAGE_TAG"  "$REPO_ROOT/backend"
  docker build -t "isovalent-control-frontend:$IMAGE_TAG" "$REPO_ROOT/frontend"
  case "$PLATFORM" in
    kind)
      KIND_NAME="$(echo "$CTX" | sed 's/^kind-//')"
      kind load docker-image "isovalent-control-backend:$IMAGE_TAG"  --name "$KIND_NAME"
      kind load docker-image "isovalent-control-frontend:$IMAGE_TAG" --name "$KIND_NAME" ;;
    minikube)
      minikube image load "isovalent-control-backend:$IMAGE_TAG"
      minikube image load "isovalent-control-frontend:$IMAGE_TAG" ;;
    *)
      warn "Loaded images only exist locally; for $PLATFORM push them to a registry the cluster can pull, or drop --build and use public images." ;;
  esac
  export IC_IMAGE_PREFIX=""            # use the bare local tag
  ok "Images built and loaded"
else
  export IC_IMAGE_PREFIX="${IMAGE_REPO%/}/"
fi

# ------------------------------------------------------------------ stack (optional)
if [ "$INSTALL_STACK" = true ]; then
  c "Installing Cilium + Hubble + Tetragon (best-effort for $PLATFORM)"
  command -v helm >/dev/null || die "--install-stack needs helm."
  if ! command -v cilium >/dev/null; then
    warn "cilium CLI not found — install it: https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/ (brew install cilium-cli)"
  else
    if ! cilium status >/dev/null 2>&1; then
      cilium install \
        --set prometheus.enabled=true --set operator.prometheus.enabled=true \
        --set hubble.metrics.enableOpenMetrics=true \
        --set hubble.metrics.enabled="{dns,drop,tcp,flow,icmp,httpV2}" \
        --wait || warn "cilium install returned non-zero; on managed CNIs (EKS aws-vpc-cni, GKE) Cilium needs a cluster-specific setup — see docs."
    fi
    cst=$(helm -n kube-system status cilium -o json 2>/dev/null | grep -o '"status":"[a-z-]*"' | head -1 || true)
    case "$cst" in *pending*) warn "clearing stuck cilium release ($cst)"; helm -n kube-system rollback cilium 2>/dev/null || true;; esac
    cilium hubble enable --ui >/dev/null 2>&1 || cilium hubble enable >/dev/null 2>&1 || warn "hubble enable skipped (already enabled or helm busy)"
    cilium status --wait || true
  fi
  helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
  helm repo update >/dev/null
  helm upgrade --install tetragon cilium/tetragon -n kube-system \
    --set tetragon.grpc.enabled=true --set tetragon.grpc.address=":54321" \
    --set tetragon.prometheus.enabled=true
  kubectl -n kube-system rollout status ds/tetragon || true
  kubectl apply -f - <<'YAML'
apiVersion: v1
kind: Service
metadata: { name: tetragon-grpc, namespace: kube-system }
spec:
  selector: { app.kubernetes.io/name: tetragon }
  ports: [{ name: grpc, port: 54321, targetPort: 54321 }]
YAML
  ok "Stack installed"
fi

# ------------------------------------------------------------------ deploy app
c "Deploying isovalent-control ($MODE mode)"
envsubst < "$MANIFEST" | kubectl apply -f -

if [ "${NEEDS_PULL_SECRET:-false}" = true ]; then
  kubectl -n "$NS" patch serviceaccount isovalent-control-backend -p '{"imagePullSecrets":[{"name":"acr-pull"}]}' >/dev/null
  kubectl -n "$NS" patch serviceaccount default -p '{"imagePullSecrets":[{"name":"acr-pull"}]}' >/dev/null
  # Same tag + new digest: force a fresh pull so the cluster gets the new build.
  kubectl -n "$NS" rollout restart deploy/isovalent-control-backend deploy/isovalent-control-frontend >/dev/null
  kubectl -n "$NS" rollout status deploy/isovalent-control-backend --timeout=180s || true
  kubectl -n "$NS" rollout status deploy/isovalent-control-frontend --timeout=180s || true
fi

if [ "$MODE" = "live" ]; then
  kubectl -n "$NS" set env deploy/isovalent-control-backend \
    IC_MODE=live \
    IC_HUBBLE_RELAY_ADDR="${IC_HUBBLE_RELAY_ADDR:-hubble-relay.kube-system.svc:80}" \
    IC_TETRAGON_ADDR="${IC_TETRAGON_ADDR:-tetragon-grpc.kube-system.svc:54321}"
fi
# Fail fast on unpullable images instead of waiting out the rollout timeout.
wait_or_explain() {
  local deploy="$1" waited=0 desired updated ready
  while [ "$waited" -lt 180 ]; do
    # Require the NEW replicas to be updated AND ready — during a rolling
    # update the old pod is still ready, so readyReplicas alone lies.
    desired=$(kubectl -n "$NS" get deploy "$deploy" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo 1)
    updated=$(kubectl -n "$NS" get deploy "$deploy" -o jsonpath='{.status.updatedReplicas}' 2>/dev/null || echo 0)
    ready=$(kubectl -n "$NS" get deploy "$deploy" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo 0)
    if [ "${updated:-0}" = "${desired:-1}" ] && [ "${ready:-0}" = "${desired:-1}" ]; then
      return 0
    fi
    local bad
    bad=$(kubectl -n "$NS" get pods -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.state.waiting.reason}{"\n"}{end}{end}' 2>/dev/null \
          | grep -E 'ImagePullBackOff|ErrImagePull|InvalidImageName' | head -1 || true)
    if [ -n "$bad" ]; then
      printf '\033[1;31m✗ Images cannot be pulled (%s)\033[0m\n' "$bad" >&2
      cat >&2 <<EOF

The cluster can't pull ${IC_IMAGE_PREFIX}isovalent-control-*:${IMAGE_TAG}.
Pick the option that matches your cluster:

  AKS + Azure Container Registry:   ./install.sh --acr <your-acr-name>
  kind / minikube (build locally):  ./install.sh --build
  your own registry:                ./install.sh --image-repo <registry>/<org>

Note: images under a private/internal GitHub repo are not publicly pullable.
EOF
      exit 1
    fi
    sleep 3
    waited=$((waited + 3))
  done
  warn "$deploy did not become ready within 180s — check: kubectl -n $NS get pods"
}
wait_or_explain isovalent-control-backend
wait_or_explain isovalent-control-frontend
ok "App is running"

# Default Tetragon policies only make sense when the stack is present.
if [ "$MODE" = "live" ] || [ "$INSTALL_STACK" = true ]; then
  if kubectl get crd tracingpolicies.cilium.io >/dev/null 2>&1; then
    c "Applying default Tetragon best-practice policies (monitor mode)"
    # non-recursive: applies the default set, skips optional/ (needs BPF LSM)
    kubectl apply -f "$REPO_ROOT/policies/tetragon/" || true
  fi
fi

# ------------------------------------------------------------------ monitoring (optional)
if [ "$WITH_MONITORING" = true ]; then
  command -v helm >/dev/null || die "--with-monitoring needs helm."
  c "Installing Prometheus + Grafana"
  # A previous interrupted run can leave a release "pending-*", which blocks
  # every later upgrade with "another operation is in progress". Clear it.
  st=$(helm -n monitoring status kube-prometheus-stack -o json 2>/dev/null | grep -o '"status":"[a-z-]*"' | head -1 || true)
  case "$st" in *pending*) warn "clearing stuck kube-prometheus-stack release ($st)"; helm -n monitoring rollback kube-prometheus-stack 2>/dev/null || helm -n monitoring uninstall kube-prometheus-stack 2>/dev/null || true;; esac
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
  helm repo update >/dev/null
  kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
  helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack -n monitoring \
    -f "$REPO_ROOT/deploy/observability/monitoring-values.yaml" \
    --wait --timeout 10m || warn "monitoring install slow/failed; re-run --with-monitoring later."
  kubectl -n monitoring create configmap ic-dashboard \
    --from-file=isovalent-control.json="$REPO_ROOT/deploy/observability/dashboards/isovalent-control.json" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl -n monitoring label configmap ic-dashboard grafana_dashboard=1 --overwrite
  # Scraping. Without this every Grafana panel renders "No data": Cilium ships
  # Hubble metrics OFF, nothing tells Prometheus to scrape Cilium/Tetragon, and
  # a ServiceMonitor selects Services by label. enable-metrics.sh fixes all
  # three and then verifies Prometheus really has the series.
  bash "$REPO_ROOT/hack/enable-metrics.sh" || warn "metrics wiring incomplete — run ./hack/enable-metrics.sh after the install"
  ok "Grafana ready — dashboard 'Isovalent Control'"
fi

# ------------------------------------------------------------------ wire embedded UIs
# Point the app at the original Hubble UI + Grafana so it can embed them.
# These are localhost port-forward URLs the user opens below.
EMBED=""
[ "$INSTALL_STACK" = true ] && EMBED="$EMBED IC_HUBBLE_UI_URL=http://localhost:12000"
[ "$WITH_MONITORING" = true ] && EMBED="$EMBED IC_GRAFANA_URL=http://localhost:3001"
if [ -n "$EMBED" ]; then
  c "Wiring embedded UIs into the app"
  # shellcheck disable=SC2086
  kubectl -n "$NS" set env deploy/isovalent-control-backend $EMBED
  kubectl -n "$NS" rollout status deploy/isovalent-control-backend --timeout=120s || true
fi

# ------------------------------------------------------------------ goat (opt-in, separate path)
if [ "$WITH_GOAT" = true ]; then
  c "Deploying Kubernetes Goat (intentionally vulnerable) into $GOAT_DIR"
  command -v git >/dev/null || die "--with-goat needs git."
  if [ ! -f "$GOAT_DIR/setup-kubernetes-goat.sh" ]; then
    git clone https://github.com/madhuakula/kubernetes-goat.git "$GOAT_DIR"
  fi
  ( cd "$GOAT_DIR" && bash setup-kubernetes-goat.sh )
  ok "Kubernetes Goat deployed (generates real traffic for live mode)"
fi

# ------------------------------------------------------------------ done
c "Done."
if [ "$INSTALL_STACK" = true ]; then
  echo "Hubble UI (embedded in Service Map): kubectl -n kube-system port-forward svc/hubble-ui 12000:80 &"
fi
if [ "$WITH_MONITORING" = true ]; then
  echo "Grafana (embedded in Dashboards):    kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3001:80 &"
  echo "  admin password: kubectl -n monitoring get secret kube-prometheus-stack-grafana -o jsonpath='{.data.admin-password}' | base64 -d"
fi
if [ "$PORT_FORWARD" = true ]; then
  echo
  # One code path for forwards: connect.sh. It picks free ports, starts fully
  # detached supervisors, verifies each one, and can be re-run any time from any
  # shell. The app itself works on any port — the frontend container proxies
  # /api and /ws to the backend Service, so nothing is baked into the browser.
  IC_NAMESPACE="$NS" IC_FE_PORT="$FE_PORT" IC_BE_PORT="$BE_PORT" \
    bash "$REPO_ROOT/connect.sh" start
  echo
  c "Reconnecting later"
  echo "  new terminal, reboot, VPN drop, laptop asleep — run:  ./connect.sh"
  echo "  check:  ./connect.sh status        stop:  ./connect.sh stop"
else
  echo "Open the UI with:"
  echo "  ./connect.sh"
fi
