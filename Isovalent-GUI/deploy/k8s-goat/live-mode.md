# Switching isovalent-control to LIVE mode

Live mode reads real data from three components. Add whichever are missing,
then point the backend at them.

## 1. Cilium as the CNI (required for network policy + Hubble flows)

Kubernetes Goat runs on whatever CNI your cluster shipped with. If `inspect.sh`
reported **no Cilium CRDs**, you need Cilium as the CNI:

- **kind:** recreate the cluster with the default CNI disabled, then
  `cilium install` (Cilium CLI). Kubernetes Goat's kind config supports this —
  set `disableDefaultCNI: true` in the kind config and install Cilium before
  deploying Goat.
- **AKS:** the supported path is **Azure CNI Powered by Cilium**, chosen at
  cluster creation:
  ```bash
  az aks create -g <rg> -n <cluster> \
    --network-plugin azure --network-dataplane cilium --network-plugin-mode overlay
  ```
  You cannot convert a kubenet/Azure-CNI cluster to Cilium in place — it's a
  create-time choice, so this means a new cluster (redeploy Goat onto it).

## 2. Enable Hubble Relay (flows + service map)

```bash
# With the Cilium CLI:
cilium hubble enable
# or Helm:
helm upgrade cilium cilium/cilium -n kube-system --reuse-values \
  --set hubble.enabled=true --set hubble.relay.enabled=true
```
Relay is then reachable in-cluster at `hubble-relay.kube-system.svc:80`.
On **AKS Powered by Cilium**, Hubble is managed by Azure — enable it per the
"Container Network Observability" docs; the relay service name may differ, so
confirm with `kubectl -n kube-system get svc | grep hubble`.

## 3. Install Tetragon (runtime security events)

```bash
helm repo add cilium https://helm.cilium.io
helm install tetragon cilium/tetragon -n kube-system \
  --set tetragon.grpc.enabled=true --set tetragon.grpc.address=":54321"
```
Expose the gRPC port with a Service so the backend can reach it:
```bash
kubectl -n kube-system expose ds tetragon --name tetragon-grpc \
  --port 54321 --target-port 54321
```
> Note: Tetragon is a DaemonSet; a single ClusterIP Service load-balances to
> **one** node's agent, so you'll see that node's events. Fine for a demo/Goat
> single-node setup; for multi-node, run the backend as a DaemonSet or use
> Tetragon's export pipeline (a Phase-2 item).

## 4. Point the backend at them + apply Cilium's policy CRDs

```bash
kubectl -n isovalent-control set env deploy/isovalent-control-backend \
  IC_MODE=live \
  IC_HUBBLE_RELAY_ADDR=hubble-relay.kube-system.svc:80 \
  IC_TETRAGON_ADDR=tetragon-grpc.kube-system.svc:54321
kubectl -n isovalent-control rollout status deploy/isovalent-control-backend
```
The ClusterRole in `isovalent-control.yaml` already grants the backend access to
the `cilium.io` policy CRDs, so the Policy Builder can list and apply
CNP/CCNP/TracingPolicy immediately.

## 5. (Optional) Secure it with OIDC

Mock/dev mode runs with **authentication disabled**. Before exposing this
anywhere shared, set an issuer so the RBAC middleware is enforced:
```bash
kubectl -n isovalent-control set env deploy/isovalent-control-backend \
  IC_OIDC_ISSUER=https://<your-issuer> IC_OIDC_CLIENT_ID=<client-id>
```
Map users to roles via a token claim: `ic:viewer`, `ic:editor:ns-a,ns-b`,
`ic:admin` (see the root README).

---

### ⚠️ Kubernetes Goat + the Policy Builder

Goat is *intentionally vulnerable* and several scenarios rely on open network
paths. If you use the Policy Builder to apply a restrictive
`CiliumNetworkPolicy` (default-deny, egress lockdown, etc.), you may break Goat
challenges — which is a great way to *demonstrate* enforcement, but do it
knowingly. Test policies in a scratch namespace first, or keep a
`kubectl delete cnp <name>` handy to roll back.
