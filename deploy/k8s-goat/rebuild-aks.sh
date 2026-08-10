#!/usr/bin/env bash
# End-to-end: build images, stand up a clean AKS cluster with Cilium + Hubble
# + Tetragon, deploy isovalent-control (live mode), and deploy Kubernetes Goat.
# Turnkey and re-runnable — every fix we hit the hard way is baked in here.
#
# Run from anywhere (paths resolve from the script location):
#   bash deploy/k8s-goat/rebuild-aks.sh
#
# Toggles (env vars):
#   FORCE_REBUILD=true   delete + recreate the cluster (default: reuse if healthy)
#   SKIP_BUILD=true      skip the ACR image build (reuse existing image tags)
#   WITH_MONITORING=true install Prometheus + Grafana (default true)
#   WITH_GOAT=true       deploy Kubernetes Goat (default false; opt-in only)
#   AUTH_IPS=a/32,b/32   API-server allow-list (default: your current public IP)
#   GOAT_DIR=/path       where to clone kubernetes-goat (default ~/kubernetes-goat)
set -euo pipefail

# -------- config (edit if you like) --------
RG=k8s-goat-eastus-rg
CLUSTER=k8s-goat-cluster
LOCATION=israelcentral
NODE_SIZE=Standard_D4ds_v4
NODE_COUNT=3
ACR=k8sgoat9129
NS=isovalent-control
IMAGE_TAG=0.2.0
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MANIFEST="$REPO_ROOT/deploy/manifests/isovalent-control.yaml"
# Where your kubernetes-goat repo lives (auto-cloned if absent).
GOAT_DIR="${GOAT_DIR:-$HOME/kubernetes-goat}"
# Org policy (Cisco "Block AKS Public Endpoint") forbids a 0.0.0.0/0 API server,
# so the API must be restricted. Default to your current public IP; if your IP
# later changes and kubectl times out, re-add it with:
#   az aks update -g $RG -n $CLUSTER --api-server-authorized-ip-ranges "$(curl -s ifconfig.me)/32"
MYIP="$(curl -s ifconfig.me)"
AUTH_IPS="${AUTH_IPS:-$MYIP/32}"

# -------- preflight --------
echo "==> Preflight: checking tools"
for t in az kubectl helm envsubst; do
  command -v "$t" >/dev/null || { echo "MISSING: $t"; exit 1; }
done
if ! command -v cilium >/dev/null; then
  echo "cilium CLI not found; installing via Homebrew..."
  brew install cilium-cli
fi
[ -f "$MANIFEST" ] || { echo "Can't find $MANIFEST — run from the isovalent-control repo."; exit 1; }

# -------- 0. build images in ACR (from the fixed Dockerfiles) --------
# Builds server-side in Azure — no local Docker needed. Uses the ACR admin
# credentials for the pull secret later (no subscription role assignment).
if [ "${SKIP_BUILD:-false}" != "true" ]; then
  echo "==> Ensuring ACR '$ACR' exists and building images"
  az acr show -n "$ACR" >/dev/null 2>&1 || az acr create -n "$ACR" -g "$RG" --sku Basic
  az acr build -r "$ACR" -t "isovalent-control-backend:$IMAGE_TAG"  "$REPO_ROOT/backend"
  az acr build -r "$ACR" -t "isovalent-control-frontend:$IMAGE_TAG" "$REPO_ROOT/frontend"
else
  echo "==> SKIP_BUILD=true — reusing existing image tags in $ACR"
fi

# -------- 1-2. ensure the cluster exists (re-runnable) --------
# Reuse an existing healthy cluster; set FORCE_REBUILD=true to delete+recreate.
if [ "${FORCE_REBUILD:-false}" != "true" ] && \
   az aks show -g "$RG" -n "$CLUSTER" --query provisioningState -o tsv 2>/dev/null | grep -q Succeeded; then
  echo "==> Reusing existing healthy cluster '$CLUSTER' (set FORCE_REBUILD=true to recreate)"
else
  if az aks show -g "$RG" -n "$CLUSTER" >/dev/null 2>&1; then
    echo "==> Deleting existing cluster"
    az aks operation-abort -g "$RG" -n "$CLUSTER" 2>/dev/null || true
    sleep 10
    az aks delete -g "$RG" -n "$CLUSTER" --yes || true
    while az aks show -g "$RG" -n "$CLUSTER" >/dev/null 2>&1; do echo "  waiting for delete..."; sleep 20; done
  fi
  # API restricted to $AUTH_IPS to satisfy the org 'no public endpoint' policy.
  echo "==> Creating fresh AKS (BYOCNI, API restricted to $AUTH_IPS). ~5-8 min"
  az aks create -g "$RG" -n "$CLUSTER" \
    --location "$LOCATION" \
    --node-count "$NODE_COUNT" \
    --node-vm-size "$NODE_SIZE" \
    --network-plugin none \
    --api-server-authorized-ip-ranges "$AUTH_IPS" \
    --generate-ssh-keys
fi
az aks get-credentials -g "$RG" -n "$CLUSTER" --overwrite-existing

# -------- 3. Cilium + Hubble --------
# BYOCNI: use Cilium's own cluster-pool IPAM + VXLAN overlay (NOT Azure IPAM),
# so there's no dependency on the cluster identity having Azure network perms.
# The node resource group is passed only to satisfy the CLI's AKS precondition.
echo "==> Installing Cilium + enabling Hubble Relay"
NODE_RG=$(az aks show -g "$RG" -n "$CLUSTER" --query nodeResourceGroup -o tsv)
if ! cilium status >/dev/null 2>&1; then
  cilium install \
    --set ipam.mode=cluster-pool \
    --set routingMode=tunnel \
    --set azure.resourceGroup="$NODE_RG" \
    --set prometheus.enabled=true \
    --set operator.prometheus.enabled=true \
    --set hubble.metrics.enableOpenMetrics=true \
    --set hubble.metrics.enabled="{dns,drop,tcp,flow,icmp,httpV2}" \
    --wait
fi
cilium hubble enable --ui || cilium hubble enable || true
cilium status --wait

# -------- 4. Tetragon (gRPC bound to :54321 so a Service can reach it) --------
echo "==> Installing Tetragon"
helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
helm repo update >/dev/null
helm upgrade --install tetragon cilium/tetragon -n kube-system \
  --set tetragon.grpc.enabled=true --set tetragon.grpc.address=":54321" \
  --set tetragon.prometheus.enabled=true
kubectl -n kube-system rollout status ds/tetragon
# A Service can't be created from a DaemonSet with `kubectl expose`; apply one
# directly, selecting the Tetragon pods so the backend can reach gRPC :54321.
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: tetragon-grpc
  namespace: kube-system
spec:
  selector:
    app.kubernetes.io/name: tetragon
  ports:
    - name: grpc
      port: 54321
      targetPort: 54321
EOF

# -------- 5. ACR pull secret (admin creds; no role assignment needed) --------
echo "==> Wiring ACR image-pull secret"
az acr update -n "$ACR" --admin-enabled true >/dev/null
ACR_USER=$(az acr credential show -n "$ACR" --query username -o tsv)
ACR_PASS=$(az acr credential show -n "$ACR" --query 'passwords[0].value' -o tsv)
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" create secret docker-registry acr-pull \
  --docker-server="$ACR.azurecr.io" --docker-username="$ACR_USER" --docker-password="$ACR_PASS" \
  --dry-run=client -o yaml | kubectl apply -f -

# -------- 6. deploy isovalent-control (live mode) --------
echo "==> Deploying isovalent-control (live)"
export IC_IMAGE_PREFIX="$ACR.azurecr.io/"
export IC_IMAGE_TAG="$IMAGE_TAG"
envsubst < "$MANIFEST" | kubectl apply -f -
kubectl -n "$NS" patch serviceaccount isovalent-control-backend -p '{"imagePullSecrets":[{"name":"acr-pull"}]}'
kubectl -n "$NS" patch serviceaccount default               -p '{"imagePullSecrets":[{"name":"acr-pull"}]}'
kubectl -n "$NS" set env deploy/isovalent-control-backend \
  IC_MODE=live \
  IC_HUBBLE_RELAY_ADDR=hubble-relay.kube-system.svc:80 \
  IC_TETRAGON_ADDR=tetragon-grpc.kube-system.svc:54321
kubectl -n "$NS" rollout restart deploy/isovalent-control-backend deploy/isovalent-control-frontend
kubectl -n "$NS" rollout status deploy/isovalent-control-backend
kubectl -n "$NS" rollout status deploy/isovalent-control-frontend

# -------- 6b. default Tetragon best-practice policies --------
echo "==> Applying default Tetragon TracingPolicies (monitor mode)"
kubectl apply -f "$REPO_ROOT/policies/tetragon/" || true

# -------- 6c. observability: Prometheus + Grafana + dashboards --------
if [ "${WITH_MONITORING:-true}" = "true" ]; then
  echo "==> Installing kube-prometheus-stack (Prometheus + Grafana)"
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
  helm repo update >/dev/null
  kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
  helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
    -n monitoring \
    -f "$REPO_ROOT/deploy/observability/monitoring-values.yaml" \
    --wait --timeout 10m || echo "  (monitoring install slow/failed; re-run or set WITH_MONITORING=false)"
  # Provision our dashboard + scrape config (official Cilium/Hubble ones come via values).
  kubectl -n monitoring create configmap ic-dashboard \
    --from-file=isovalent-control.json="$REPO_ROOT/deploy/observability/dashboards/isovalent-control.json" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl -n monitoring label configmap ic-dashboard grafana_dashboard=1 --overwrite
  bash "$REPO_ROOT/hack/enable-metrics.sh" || true
else
  echo "==> WITH_MONITORING=false — skipping Prometheus/Grafana"
fi

# -------- 6d. wire embedded UIs (Hubble UI + Grafana) into the app --------
echo "==> Wiring embedded Hubble UI + Grafana into the app"
kubectl -n "$NS" set env deploy/isovalent-control-backend \
  IC_HUBBLE_UI_URL=http://localhost:12000 \
  IC_GRAFANA_URL=http://localhost:3001 || true

# -------- 7. Kubernetes Goat (OPT-IN) --------
# Intentionally vulnerable — installed only when WITH_GOAT=true, cloned into a
# separate folder from its GitHub so it never mixes with the platform.
if [ "${WITH_GOAT:-false}" = "true" ]; then
  echo "==> Deploying Kubernetes Goat (WITH_GOAT=true)"
  if [ ! -f "$GOAT_DIR/setup-kubernetes-goat.sh" ]; then
    echo "  cloning kubernetes-goat into $GOAT_DIR"
    git clone https://github.com/madhuakula/kubernetes-goat.git "$GOAT_DIR"
  fi
  ( cd "$GOAT_DIR" && bash setup-kubernetes-goat.sh )
  kubectl get pods -A | grep -Ei 'goat|health-check|metadata-db|hunger-check|batch-check' || true
else
  echo "==> Skipping Kubernetes Goat (set WITH_GOAT=true to deploy it)"
fi

cat <<EOF

==============================================================
DONE. Fresh cluster with Cilium + Hubble + Tetragon,
Kubernetes Goat, default Tetragon policies, Prometheus +
Grafana, and isovalent-control (v$IMAGE_TAG) live.

Open the UI + the embedded Hubble UI and Grafana (run all four):
  kubectl -n $NS port-forward svc/isovalent-control-frontend 3000:3000 &
  kubectl -n $NS port-forward svc/isovalent-control-backend  8081:8081 &
  kubectl -n kube-system  port-forward svc/hubble-ui 12000:80 &          # Service Map tab
  kubectl -n monitoring   port-forward svc/kube-prometheus-stack-grafana 3001:80 &  # Dashboards tab
  open http://localhost:3000

Grafana admin password:
  kubectl -n monitoring get secret kube-prometheus-stack-grafana -o jsonpath='{.data.admin-password}' | base64 -d; echo

In the app: Service Map = embedded Hubble UI; Dashboards = embedded
Grafana (Cilium/Hubble/Tetragon official + our dashboard); Runtime
Policies = Monitor/Kill/Remove; Network Policies = build + dry-run +
GitOps PR; Security Events = 14-day enforcement log; History = time-travel.
==============================================================
EOF
