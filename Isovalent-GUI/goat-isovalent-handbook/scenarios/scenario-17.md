# Scenario 17 — KubeAudit

**Coverage:** 🧩 Complementary · 🔍 Identify · Control: **Posture (Enterprise), not enforcement**
**Target:** `kubeaudit` — a cluster manifest auditing *tool*

## What this scenario is
Run kubeaudit to find insecure workload settings (runAsNonRoot missing,
privileged, allowPrivilegeEscalation, missing security contexts, etc.). An
audit, not an attack.

## Where Isovalent fits
Kubeaudit reports **what's misconfigured**; Isovalent is part of **enforcing the
fix at runtime**:
- Findings about privilege/capabilities/socket exposure map directly to Tetragon
  runtime detections (scenarios 2, 4, 12, 18 policies).
- Findings about missing network isolation map to Cilium default-deny
  (`policies/network/00-baseline-default-deny.yaml`).
- **Isovalent Enterprise** posture/observability can track these continuously
  rather than as a one-shot CLI run.

## Talk track
> "Kubeaudit is a great pre-flight check — run it in CI. The gap it leaves is
> runtime: it can't tell you if a compliant-looking pod does something bad after
> it's scheduled. That's where Tetragon and Cilium turn audit findings into live,
> enforced controls."

## Honesty
This is a tooling scenario. Isovalent complements it (enforcement + continuous
posture); it does not replace manifest auditing or admission control. Enforce the
audit's findings with Kyverno at admission (scenario 22) plus Isovalent at
runtime.
