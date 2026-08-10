#!/usr/bin/env bash
# Inspect the current kubectl context to decide the deployment path for
# isovalent-control. Read-only: runs only `get`/`version` calls.
set -uo pipefail

line() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }
have() { kubectl get "$@" >/dev/null 2>&1; }

line "Current context"
kubectl config current-context 2>/dev/null || { echo "no kubectl context set"; exit 1; }

line "Cluster / nodes"
kubectl version -o yaml 2>/dev/null | grep -E 'gitVersion' | head -2
kubectl get nodes -o wide 2>/dev/null

# --- What kind of cluster is this? -----------------------------------------
line "Cluster type (drives image loading)"
NODE0=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
PROVIDER=$(kubectl get nodes -o jsonpath='{.items[0].spec.providerID}' 2>/dev/null)
CLUSTER_TYPE="unknown"
case "$NODE0$PROVIDER" in
  *kind*)        CLUSTER_TYPE="kind" ;;
  *minikube*)    CLUSTER_TYPE="minikube" ;;
  *azure*|*aks*) CLUSTER_TYPE="aks" ;;
esac
echo "detected: $CLUSTER_TYPE   (node0=$NODE0 providerID=$PROVIDER)"
echo "$CLUSTER_TYPE" > /tmp/ic-cluster-type

case "$CLUSTER_TYPE" in
  kind)     echo "→ images load with:  kind load docker-image <img>" ;;
  minikube) echo "→ images load with:  minikube image load <img>" ;;
  aks)      echo "→ AKS nodes cannot be side-loaded; you need a registry (see build-and-load.sh, aks branch)" ;;
  *)        echo "→ could not classify; treat like AKS (registry required) unless you know it's local" ;;
esac

# --- CNI -------------------------------------------------------------------
line "CNI / Cilium"
kubectl -n kube-system get ds,deploy 2>/dev/null | grep -Ei 'cilium|azure-cni|calico|kube-proxy|flannel' || echo "no obvious CNI pods matched"
if have crd ciliumnetworkpolicies.cilium.io; then
  echo "✓ Cilium CRDs present (CiliumNetworkPolicy available)"
else
  echo "✗ Cilium CRDs NOT found — network-policy features need Cilium as the CNI"
fi

# --- Hubble ----------------------------------------------------------------
line "Hubble Relay"
if kubectl -n kube-system get svc hubble-relay >/dev/null 2>&1; then
  kubectl -n kube-system get svc hubble-relay
  echo "✓ Hubble Relay service present → IC_HUBBLE_RELAY_ADDR=hubble-relay.kube-system.svc:80"
else
  echo "✗ hubble-relay service not found — enable Hubble to get flows (see live-mode.md)"
fi

# --- Tetragon --------------------------------------------------------------
line "Tetragon"
if have crd tracingpolicies.cilium.io; then
  echo "✓ TracingPolicy CRD present"
  kubectl get ds -A 2>/dev/null | grep -i tetragon || echo "  (CRD present but no tetragon DaemonSet found)"
else
  echo "✗ Tetragon not installed — runtime-security events need it (see live-mode.md)"
fi

# --- Kubernetes Goat -------------------------------------------------------
line "Kubernetes Goat"
kubectl get ns 2>/dev/null | grep -Ei 'goat|big-monolith|secure-middleware' || true
kubectl get pods -A 2>/dev/null | grep -Ei 'goat|health-check|hunger-check|metadata-db|batch-check' | head || echo "no obvious Kubernetes Goat workloads matched (it may use the default namespace)"

# --- Verdict ---------------------------------------------------------------
line "Verdict"
HAS_CILIUM=no; HAS_HUBBLE=no; HAS_TET=no
have crd ciliumnetworkpolicies.cilium.io && HAS_CILIUM=yes
kubectl -n kube-system get svc hubble-relay >/dev/null 2>&1 && HAS_HUBBLE=yes
have crd tracingpolicies.cilium.io && HAS_TET=yes
echo "cilium=$HAS_CILIUM hubble=$HAS_HUBBLE tetragon=$HAS_TET  cluster=$CLUSTER_TYPE"
if [ "$HAS_CILIUM$HAS_HUBBLE$HAS_TET" = "yesyesyes" ]; then
  echo "→ Ready for LIVE mode. Deploy isovalent-control.yaml, then apply live-mode patch."
else
  echo "→ Start in MOCK mode now (works today, no dependencies)."
  echo "  Add the missing pieces from live-mode.md when you want real data."
fi
