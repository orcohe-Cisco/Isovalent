# Scenario 16 — RBAC least-privilege misconfiguration

**Coverage:** 🛑 Block (network) · 🔍 Identify · Control: **Cilium API-server egress deny + Tetragon token detect**
**Target:** `hunger-check` in `big-monolith` (over-permissive ServiceAccount)

## The attack
The pod's ServiceAccount has more RBAC than it needs. The attacker uses the
mounted token to hit the API server and read secrets:

```bash
export TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
export CACERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
curl --cacert $CACERT -H "Authorization: Bearer $TOKEN" \
  https://${KUBERNETES_SERVICE_HOST}/api/v1/namespaces/big-monolith/secrets | grep k8svaultapikey
```

## Identify — two signals
**Tetragon:** the token read by a shell/curl (scenario 12 policy):
```bash
kubectl apply -f policies/tetragon/12-16-serviceaccount-token.yaml
```
**Hubble:** the pod talking to the `kube-apiserver` identity — unusual for most
app workloads:
```bash
hubble observe --from-label app=hunger-check --to-identity kube-apiserver --follow
```

## Block (Cilium network defence-in-depth)
```bash
kubectl apply -f policies/network/16-apiserver-egress-restrict.yaml
```
This **denies egress to the `kube-apiserver` entity** for `hunger-check`. Even
with a valid, over-permissive token, the pod cannot reach the API server to use
it — the curl drops.

## Primary fix — RBAC
Scope the ServiceAccount: replace the wildcard/secret-reading Role with least
privilege. The Cilium policy is the network backstop for when RBAC is too broad
(and it usually is, somewhere).

## Talk track
> "RBAC is the right fix — and it's also the thing everyone gets slightly wrong.
> Cilium adds a second lock: this workload has no business talking to the API
> server, so we deny that egress entirely. Now a stolen, over-privileged token is
> useless *from this pod* — and the attempt to use it is a Hubble alert on the
> kube-apiserver identity."

## Limitations / honesty
If the workload legitimately needs the API server, you can't blanket-deny it —
tighten RBAC and use L7 Kubernetes-aware policy / Tetragon detection instead.
Network egress deny is cleanest for the many workloads that never need the API.
