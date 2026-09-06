# Scenario 5 — Docker CIS benchmarks analysis

**Coverage:** 🧩 Complementary · 🔍 Identify · Control: **Tetragon telemetry (not a "block")**
**Target:** `docker-bench-security` — a scanning *tool*, not an exploit

## What this scenario is
This is an **analysis** scenario: you run docker-bench-security to audit the
node's Docker/containerd configuration against the CIS benchmark. There is no
attacker action to "block" — the deliverable is a hardening report.

## Where Isovalent fits
Tetragon can surface, at runtime, the very misconfigurations CIS flags — most
usefully **containers that mount the runtime socket or run privileged**. Reuse
the socket-access policy from scenario 2:

```bash
kubectl apply -f policies/tetragon/02-container-socket-abuse.yaml
# now any workload touching docker.sock/containerd.sock is an event (or a kill)
```

So instead of a point-in-time benchmark, you get **continuous** enforcement of
one of the benchmark's key controls.

## Talk track
> "docker-bench gives you a snapshot of node hardening. Tetragon turns the most
> important findings — socket exposure, privileged exec — into continuous,
> enforced runtime controls. They're complementary: run the benchmark to know
> your posture, run Tetragon so drift from it gets caught the moment it happens."

## Honesty
Do **not** present Cilium/Tetragon as a replacement for CIS benchmarking. The
benchmark checks daemon config, file permissions, and audit settings that a CNI
doesn't touch. Isovalent enforces a subset (socket/privilege behaviour) at
runtime; the rest is node hardening + posture tooling (Isovalent Enterprise
posture reporting if the customer has it).
