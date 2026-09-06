# Scenario 13 — DoS the Memory/CPU resources

**Coverage:** 🛑 Block (runtime) · 🔍 Identify · Control: **Tetragon kill + LimitRange (primary)**
**Target:** `hunger-check-deployment`, `big-monolith` namespace (no resource limits)

## The attack
The pod has no resource requests/limits, so it can consume the whole node:

```bash
stress-ng --vm 2 --vm-bytes 2G --timeout 30s
kubectl -n big-monolith top pod hunger-check-deployment-xxxx   # runaway usage
```

## Identify (Tetragon)
The `stress-ng` exec is caught with its full command line and pod identity:

```bash
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact --pods hunger-check
# ⚡ process_exec /usr/bin/stress-ng --vm 2 --vm-bytes 2G
```

## Block (Tetragon runtime)
```bash
kubectl apply -f policies/tetragon/13-dos-stressng.yaml
# Sigkill stress-ng / stress the instant it execs
```

## Primary fix — resource governance
The correct control is Kubernetes-native: set requests/limits and a namespace
`LimitRange` / `ResourceQuota` so no single pod can starve the node. Enforce that
they're always present with Kyverno (scenario 22). Example LimitRange:

```yaml
apiVersion: v1
kind: LimitRange
metadata: { name: default-limits, namespace: big-monolith }
spec:
  limits:
    - default: { cpu: "500m", memory: "512Mi" }
      defaultRequest: { cpu: "100m", memory: "128Mi" }
      type: Container
```

## Talk track
> "Resource limits are the real fix, and you should require them at admission.
> But limits are a config you can forget — Tetragon is the runtime backstop that
> kills a stress bomb even in a pod someone shipped without limits. Defence in
> depth: policy at admission, enforcement at runtime."

## Limitations / honesty
Killing `stress-ng` is signature-based; a determined attacker can consume
resources with a renamed binary or in-app allocation. The durable control is
requests/limits + quota. Present Tetragon here as a catch for the obvious case,
not the whole answer.
