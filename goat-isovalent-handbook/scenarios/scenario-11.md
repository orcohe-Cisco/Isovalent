# Scenario 11 — Kubernetes namespace bypass ⭐ flagship demo

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Cilium namespace isolation + Tetragon recon detection**
**Target:** Redis `cache-store` in `secure-middleware` ns, attacked from `default`

## The attack
Kubernetes networking is flat — namespaces are **not** a security boundary. A
pod in `default` scans the cluster and reaches Redis in another namespace:

```bash
kubectl run -it hacker-container --image=madhuakula/hacker-container -- sh
zmap -p 6379 10.0.0.0/8 -o results.csv          # find Redis anywhere
redis-cli -h cache-store-service.secure-middleware
KEYS *
GET SECRETSTUFF                                 # cross-namespace secret
```

## Identify
**Hubble** shows the scan as a fan of DROPPED/attempted SYNs from one source, and
the cross-namespace Redis hit explicitly:

```bash
hubble observe --to-label app=cache-store --follow
hubble observe --from-label app=hacker-container --verdict DROPPED --follow
```

**Tetragon** names the tools (`zmap`, `redis-cli`) as they exec:

```bash
kubectl apply -f policies/tetragon/11-recon-tools.yaml
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact
# ⚡ process_exec /usr/bin/zmap -p 6379 10.0.0.0/8
```

## Block (Cilium namespace isolation)
```bash
kubectl apply -f policies/network/11-namespace-isolation.yaml
```
- **Lock the target:** `cache-store` accepts `:6379` only from its own namespace.
- **Contain the source:** `default` gets default-deny egress (DNS + intra-ns only).

Re-run: the zmap sweep lands nothing and `redis-cli` to another namespace times
out — DROPPED in Hubble.

## Talk track
> "This is the myth that namespaces are a security boundary. By default they
> aren't — watch this pod in `default` read a secret out of Redis in
> `secure-middleware`. Now one Cilium policy: Redis only answers its own
> namespace, and the attacker's cluster-wide scan lights up red in Hubble and
> then hits a wall. *That's* a boundary — identity-based, not IP-based."

## Limitations / honesty
None material — this is squarely Cilium's core value and the single best
Isovalent demo in GOAT. If you show one scenario, show this one.
