# Scenario 19 — Popeye cluster sanitizer

**Coverage:** 🧩 Complementary · 🔍 Identify · Control: **Posture (Enterprise), not enforcement**
**Target:** `popeye` — a read-only cluster hygiene *scanner*

## What this scenario is
Popeye scans for cluster smells: unused resources, missing probes, no resource
limits, over-broad RBAC, missing network policies, etc. A sanity report.

## Where Isovalent fits
Popeye will flag two categories Isovalent directly addresses:
- **"No NetworkPolicies / everything is open"** → apply Cilium default-deny and
  build allow-lists from observed Hubble flows
  (`policies/network/00-baseline-default-deny.yaml`). Isovalent Enterprise policy
  **recommendation** can auto-generate these from real traffic.
- **"No resource limits"** → LimitRange (scenario 13) + Tetragon runtime catch.

## Talk track
> "Popeye is a good nag — it tells you where the hygiene gaps are. The most
> common one it screams about is 'no network policies, your cluster is flat.'
> That's exactly what we fixed in scenario 11. Isovalent Enterprise can even
> watch your real traffic and *generate* the least-privilege policy for you, so
> closing the Popeye finding isn't a hand-written-YAML slog."

## Honesty
A reporting tool, not an attack. Isovalent enforces a subset of what Popeye
flags (segmentation, limits); the rest (unused resources, probes, labels) is
general hygiene outside a CNI's scope.
