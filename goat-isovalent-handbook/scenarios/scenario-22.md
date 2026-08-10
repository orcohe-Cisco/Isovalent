# Scenario 22 — Kyverno policy engine (admission control)

**Coverage:** 🧩 Complementary · 🔍 Identify · Control: **Admission (Kyverno) pairs with Isovalent runtime**
**Target:** GOAT's Kyverno scenario — validate/mutate/generate at admission time

## What this scenario is
Kyverno enforces policy at the **admission** boundary — before a pod is
scheduled. It can reject privileged pods, require signed images, block hostPath,
mandate resource limits, etc. This is the layer that *prevents* many earlier
scenarios from ever deploying.

## The key message: admission + runtime are complementary, not competing
| Layer | Tool | Stops |
|-------|------|-------|
| **Admission** (before scheduling) | Kyverno | the privileged pod (s4), the socket mount (s2), the limitless pod (s13), the unsigned image (s10/s15) from ever being created |
| **Runtime** (after scheduling) | Tetragon | what a pod *does* once running — even a pod that passed admission |
| **Network** | Cilium/Hubble | which flows are allowed, and full visibility |

The honest positioning: **Kyverno is the front door, Isovalent is what happens
inside the house.** A pod that legitimately needs a capability passes admission
but is still watched and constrained at runtime by Tetragon; its traffic is still
governed by Cilium.

## Example: the two layers on scenario 4 (container escape)
1. **Kyverno (prevent):** deny `privileged: true` and `hostPath: /` at admission —
   the escape pod never schedules.
2. **Tetragon (detect+respond):** for the monitoring agent that genuinely needs
   elevated caps and *is* allowed in, kill it if it reads the node kubeconfig
   (`policies/tetragon/04-container-escape.yaml`).

## Talk track
> "This isn't Kyverno *or* Isovalent — you want both. Kyverno stops the obviously
> bad pod at the door: no privileged, no host mounts, signed images only. But
> plenty of legitimate workloads need *some* privilege, and admission can't see
> what they do at 3am. That's Tetragon and Cilium — runtime enforcement and
> network policy for the pods that made it inside. Front door and interior."

## Honesty
Admission control is genuinely the right place to prevent most misconfig-based
scenarios — don't position Isovalent as a replacement for it. The winning story
is the layered one: Kyverno at admission, Isovalent at runtime and on the network.
