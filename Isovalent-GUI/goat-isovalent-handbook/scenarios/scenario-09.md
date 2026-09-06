# Scenario 9 — Helm v2 Tiller to PwN the cluster (deprecated)

**Coverage:** 🛑 Block · 🔍 Identify · Control: **Cilium port/identity policy**
**Status:** GOAT marks this **deprecated** (Helm v3 removed Tiller). Include only
if your cluster still runs Tiller.

## The attack
Helm v2's Tiller pod listens on `:44134` with cluster-admin and no auth. Any pod
that can reach it can install charts and take over the cluster:

```bash
helm --host tiller-deploy.kube-system:44134 install ... # arbitrary, as cluster-admin
```

## Identify (Hubble)
Any pod connecting to `tiller-deploy:44134` is almost certainly malicious:

```bash
hubble observe --to-label app=helm --to-port 44134 --follow
```

## Block (Cilium)
Deny all ingress to the Tiller identity except the CI/CD identity that legit
uses it (ideally: remove Tiller entirely). Pattern:

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: lock-tiller
  namespace: kube-system
spec:
  endpointSelector:
    matchLabels:
      app: helm            # Tiller pod labels
  ingress:
    - fromEndpoints:
        - matchLabels:
            app: ci-deployer     # only the deployer identity
      toPorts:
        - ports: [{ port: "44134", protocol: TCP }]
```

## Talk track
> "The real answer is 'you're on Helm 3, Tiller is gone.' If any Tiller is still
> breathing, Cilium lets you fence it to exactly one deployer identity while you
> remove it — nobody else in the cluster can even open the port."

## Honesty
This is a legacy scenario. Lead with migration to Helm v3; the Cilium policy is a
temporary containment, not the fix.
