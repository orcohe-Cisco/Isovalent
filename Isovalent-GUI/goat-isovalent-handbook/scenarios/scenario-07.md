# Scenario 7 — Attacking the private registry

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Cilium L7 HTTP (deny catalog) + identity ingress**
**Target:** `poor-registry` deployment, `default` namespace (`http://127.0.0.1:1235`)

## The attack
An unauthenticated Docker Registry v2 API. The attacker enumerates every repo
and reads image manifests to find embedded secrets:

```bash
curl http://127.0.0.1:1235/v2/_catalog                       # list ALL repos
curl http://127.0.0.1:1235/v2/madhuakula/k8s-goat-users-repo/manifests/latest | grep -i env
```

## Identify (Hubble L7)
Legitimate image pulls hit `/manifests/` and `/blobs/`. A human poking the
registry hits `/v2/_catalog` and `/tags/list` — enumeration signatures:

```bash
hubble observe --to-label app=poor-registry --type l7 --follow
# GET /v2/_catalog   <- nobody pulls images this way
```

## Block (Cilium L7 + identity)
```bash
kubectl apply -f policies/network/07-registry-restrict.yaml
```
- **Identity ingress:** only `app=kubernetes-goat-home` (the legit puller) may
  reach the registry; every other pod is dropped at L3/L4.
- **L7 allow-list:** even that client may only use pull verbs
  (`/v2/`, `/manifests/`, `/blobs/`) — `/v2/_catalog` enumeration returns 403.

## Talk track
> "A registry only needs to answer a couple of endpoints for real pulls. We lock
> ingress to the one workload that pulls, and then allow-list just the pull
> verbs. The attacker's `_catalog` sweep becomes a 403 and a Hubble alert instead
> of a directory of your images."

## Limitations / honesty
The underlying fix is authN/authZ on the registry. Cilium contains an exposed or
misconfigured registry by controlling *who* reaches it and *which endpoints* —
strong defence-in-depth, not a substitute for registry auth.
