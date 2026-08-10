# Scenario 18 — Falco → Tetragon (runtime security monitoring)

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Tetragon IS the Isovalent runtime engine**
**Target:** the GOAT Falco scenario — reproduced and extended with Tetragon

## What this scenario is
GOAT uses **Falco** to demonstrate eBPF runtime detection (shell in a container,
sensitive file reads, etc.). This is the natural place to show **Tetragon**, the
Isovalent runtime engine, as the direct counterpart — with the added ability to
**enforce (Sigkill/Override)** in-kernel, not just alert.

## Tetragon parity + enforcement
```bash
kubectl apply -f policies/tetragon/18-falco-equivalents.yaml
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact
```
The bundled policy reproduces the classic Falco rules:
- **Shell in a container** — exec of `/bin/sh`, `/bin/bash`, `/bin/dash`
- **Sensitive file read** — open of `/etc/shadow`

Start in observe (`Post`) mode to mirror Falco's alerting, then flip a selector
to `Sigkill` to show what Tetragon adds: it doesn't just *report* the shell, it
can *kill* it in the kernel before it does anything.

## Falco vs Tetragon — the honest comparison
| | Falco | Tetragon |
|---|---|---|
| Detection (eBPF) | ✅ | ✅ |
| In-kernel enforcement (kill/override) | ❌ (alerts only) | ✅ Sigkill / Override |
| Kubernetes identity on events | ✅ | ✅ (rich pod/label/ancestry) |
| Part of the CNI platform | ❌ | ✅ (shares Cilium identity model) |

## Talk track
> "GOAT teaches this with Falco, and Falco is good at detection. Tetragon does
> the same detection on the same eBPF foundation, then goes one step further —
> it can Sigkill the process in-kernel the moment the rule matches. Same event,
> plus a response. And it's the same platform enforcing your network policy, so
> the identity model is shared end to end."

## Honesty
Falco is a strong, popular tool — don't trash it. The differentiator to state
plainly is **in-kernel enforcement** and **platform integration with Cilium
identity**, not raw detection capability.
