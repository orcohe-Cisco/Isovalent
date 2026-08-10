# Scenario 6 — Kubernetes CIS benchmarks analysis

**Coverage:** 🧩 Complementary · 🔍 Identify · Control: **Posture (primarily Enterprise), not enforcement**
**Target:** `kube-bench` — cluster configuration audit *tool*

## What this scenario is
Run kube-bench to check the control-plane and node config against the CIS
Kubernetes benchmark (API server flags, kubelet config, RBAC defaults, etc.).
Again: an audit, nothing to "block".

## Where Isovalent fits
Two honest connections:
1. **Network-related CIS controls become enforceable with Cilium.** The benchmark
   recommends network segmentation / default-deny (CIS 5.3.x "network policies").
   Cilium is how you actually satisfy those — see the default-deny baseline in
   `policies/network/00-baseline-default-deny.yaml`.
2. **Isovalent Enterprise** ships posture/observability that can track drift over
   time rather than a one-off scan.

```bash
# Satisfy the CIS "apply network policies to all namespaces" control:
kubectl apply -f policies/network/00-baseline-default-deny.yaml
```

## Talk track
> "kube-bench will tell you 'no network policies applied' — that's a real CIS
> finding. Cilium is the fix, not just a detection: default-deny per namespace,
> then allow-list from observed flows. So this scenario is where you show that
> Isovalent closes the exact gaps the benchmark surfaces around segmentation."

## Honesty
Most CIS Kubernetes controls (API server flags, etcd encryption, admission
config) are **not** in Cilium/Tetragon's domain. Claim only the segmentation and
runtime controls; point the rest at cluster hardening and the customer's posture
tooling.
