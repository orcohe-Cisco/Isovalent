#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Apply every defensive policy in this handbook. Run AFTER Kubernetes GOAT is
# deployed and after you have watched the attacks in Hubble/Tetragon at least
# once (so the demo shows before -> after). Namespaces must already exist.
# ---------------------------------------------------------------------------
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo ">> Tetragon runtime policies (cluster-scoped)"
kubectl apply -f "${ROOT}/policies/tetragon/"

echo ">> Cilium network / L7 / DNS policies"
kubectl apply -f "${ROOT}/policies/network/"
kubectl apply -f "${ROOT}/policies/dns-l7/"

echo ">> Egress gateway / encryption (edit node labels first!)"
echo "   Skipping auto-apply of policies/egress/ — review node selectors, then:"
echo "     kubectl apply -f ${ROOT}/policies/egress/egress-gateway-and-encryption.yaml"

echo ">> Current Cilium policies:"
kubectl get cnp,ccnp -A
echo ">> Current Tetragon policies:"
kubectl get tracingpolicy
