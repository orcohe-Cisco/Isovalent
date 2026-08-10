#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Install Cilium + Hubble + Tetragon with everything this handbook uses turned
# on (L7 proxy, DNS proxy, host firewall, Hubble). Tested against Cilium 1.16+.
# Adjust the version pins to match your cluster / Isovalent Enterprise build.
# ---------------------------------------------------------------------------
set -euo pipefail

CILIUM_VERSION="${CILIUM_VERSION:-1.16.5}"
TETRAGON_VERSION="${TETRAGON_VERSION:-1.3.0}"

echo ">> Adding Helm repos"
helm repo add cilium https://helm.cilium.io >/dev/null
helm repo add isovalent https://helm.isovalent.com >/dev/null 2>&1 || true
helm repo update >/dev/null

echo ">> Installing Cilium ${CILIUM_VERSION} (L7 + DNS proxy + Hubble + host firewall)"
helm upgrade --install cilium cilium/cilium \
  --version "${CILIUM_VERSION}" \
  --namespace kube-system \
  --set hubble.enabled=true \
  --set hubble.relay.enabled=true \
  --set hubble.ui.enabled=true \
  --set hostFirewall.enabled=true \
  --set l7Proxy=true \
  --set encryption.enabled=false   # flip to true + type=wireguard to demo encryption

echo ">> Installing Tetragon ${TETRAGON_VERSION}"
helm upgrade --install tetragon cilium/tetragon \
  --version "${TETRAGON_VERSION}" \
  --namespace kube-system

echo ">> Waiting for Cilium to be ready"
kubectl -n kube-system rollout status ds/cilium --timeout=180s

cat <<'EOF'

Done. Useful commands:
  cilium status
  kubectl -n kube-system exec ds/cilium -- cilium status | grep -i proxy
  hubble observe --follow
  kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact

Enterprise (Isovalent) extras to enable when available:
  - Hubble Timescape (historical flows)   - network policy recommendation
  - Hubble Enterprise UI / process viz     - SIEM / syslog export
EOF
