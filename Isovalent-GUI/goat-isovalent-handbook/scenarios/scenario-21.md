# Scenario 21 — Cilium Tetragon (eBPF security observability & enforcement)

**Coverage:** 🛑 Block · 🔍 Identify · Control: **This scenario *is* Isovalent — go deep**
**Target:** GOAT's own Tetragon scenario

## What this scenario is
GOAT ships a scenario dedicated to Tetragon. This is your home turf — use it to
give the customer the full mental model, then tie it back to every runtime
defence used elsewhere in this handbook (scenarios 2, 4, 10, 11, 12, 13, 16, 18).

## The three things to demo
**1. Observability (no policy).** Tetragon streams process execs, file access,
network connects with full Kubernetes identity, out of the box:
```bash
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact
```

**2. Targeted detection (TracingPolicy, `Post`).** Watch a specific sensitive
action — e.g. any read of `/etc/shadow` or a shell spawned in a container:
```bash
kubectl apply -f policies/tetragon/18-falco-equivalents.yaml
```

**3. In-kernel enforcement (`Sigkill` / `Override`).** The differentiator: kill
the process the instant it acts, before the syscall completes its damage:
```bash
kubectl apply -f policies/tetragon/13-dos-stressng.yaml   # Sigkill stress-ng
kubectl apply -f policies/tetragon/02-container-socket-abuse.yaml   # Sigkill socket access
```

## How it works (one paragraph for the whiteboard)
Tetragon attaches eBPF programs to kernel hooks (kprobes, tracepoints, LSM). When
a hook matches your selector (`matchArgs`, `matchBinaries`, `matchCapabilities`),
it fires a `matchAction`: `Post` (emit an event), `Sigkill` (kill the process),
`Override` (make the syscall return an error like `-EPERM`), or `Signal`. Because
it runs in-kernel, enforcement happens synchronously with the syscall — there's no
userspace race for the attacker to win.

## Talk track
> "Everything we've killed and detected in the other scenarios runs on this. The
> headline: Tetragon doesn't just watch, it can Sigkill or make a syscall return
> EPERM from inside the kernel. Detection *and* prevention on one eBPF
> foundation, sharing Cilium's identity model. That's the pitch — one platform
> for the network flow and the process that opened it."

## Honesty
Enforcement is powerful but signature/selector-based — model the behaviours you
care about, and start in `Post` mode to tune out false positives before flipping
to `Sigkill`. Say that; customers respect that you'll help them roll it out
safely rather than break prod on day one.
