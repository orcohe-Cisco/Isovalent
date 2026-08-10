# Scenario 14 — Hacker container preview

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Cilium default-deny + Tetragon tool detection**
**Target:** the `madhuakula/hacker-container` pentest toolbox running in-cluster

## The attack
This scenario just runs a fully-loaded pentest container in the cluster — the
attacker's staging ground for every other scenario (scanning, redis-cli, curl to
the API, etc.):

```bash
kubectl run -it hacker-container --image=madhuakula/hacker-container -- sh
# nmap, zmap, redis-cli, kubectl, curl, ... all preinstalled
```

## Identify (Tetragon + Hubble)
Every tool the box runs is a Tetragon exec event; every scan is a Hubble flow.
Reuse the recon-tools policy:

```bash
kubectl apply -f policies/tetragon/11-recon-tools.yaml
hubble observe --from-label run=hacker-container --follow
```

## Block (Cilium default-deny)
A pentest container is only useful if it can reach other things. Namespace
default-deny (from the baseline) neuters it — it can resolve DNS and nothing
else until you allow-list:

```bash
kubectl apply -f policies/network/00-baseline-default-deny.yaml   # scope to the target ns
```
Now `nmap`/`redis-cli`/`curl` from the box all drop. Optionally flip the
recon-tools Tetragon policy to `Sigkill` to kill the tools on exec.

## Talk track
> "The hacker container is the attacker's Swiss Army knife. In a flat network
> it's devastating. Under Cilium default-deny it's a box that can talk to DNS and
> nothing else — and every tool it tries to run is a named Tetragon event. You've
> turned their toolbox into a tripwire."

## Limitations / honesty
Admission control should stop an arbitrary attacker image running at all. This
page assumes it's already in — which is the realistic post-compromise case — and
shows how Isovalent contains and surfaces it.
