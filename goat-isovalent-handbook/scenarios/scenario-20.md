# Scenario 20 — Secure network boundaries (upgrade NSP to Cilium)

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Cilium — show the upgrade over stock NetworkPolicy**
**Target:** the GOAT `website-deny` NetworkPolicy exercise

## What this scenario is
GOAT teaches vanilla Kubernetes `NetworkPolicy`: create `website`, prove it's
reachable, apply a deny-ingress policy, prove it's blocked.

```bash
kubectl run --image=nginx website --labels app=website --expose --port 80
kubectl run --rm -it --image=alpine temp -- sh -c 'wget -qO- http://website'   # works
kubectl apply -f - <<'EOF'
kind: NetworkPolicy
apiVersion: networking.k8s.io/v1
metadata: { name: website-deny }
spec: { podSelector: { matchLabels: { app: website } }, ingress: [] }
EOF
# now the wget times out
```

## The Cilium upgrade (the actual demo)
This is your chance to show what Cilium adds over stock NetworkPolicy:

```bash
kubectl apply -f policies/network/20-website-deny-cilium.yaml
```
- Same deny, expressed as a `CiliumNetworkPolicy`, **plus**
- an identity-based allow: only `app=frontend` may reach `website`, and only
  `GET /` at **L7** — every other method/path returns 403.

And unlike stock NetworkPolicy, you can *see* the verdicts:

```bash
hubble observe --to-label app=website --type l7 --follow
```

## Talk track
> "Stock NetworkPolicy is all-or-nothing at L3/L4, and it's invisible — you can't
> see what it dropped. Cilium keeps the same model but adds identity and L7: allow
> exactly one workload to do exactly one HTTP verb, and watch every allowed and
> denied request in Hubble. Same CRD shape, a lot more control and visibility."

## Honesty
The GOAT exercise itself is the fix — this scenario is a feature showcase, not a
vulnerability. Use it to contrast native NetworkPolicy with Cilium's identity +
L7 + observability.
