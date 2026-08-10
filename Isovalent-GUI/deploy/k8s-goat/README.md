# Running isovalent-control on your AKS + Kubernetes Goat cluster

## Turnkey: one script (recommended)

`rebuild-aks.sh` does everything end-to-end and is idempotent — build images in
ACR, stand up a clean AKS cluster with Cilium + Hubble + Tetragon, deploy
isovalent-control in live mode, and deploy Kubernetes Goat:

```bash
GOAT_DIR=~/kubernetes-goat bash deploy/k8s-goat/rebuild-aks.sh
```

Every issue found the hard way is baked in: BYO-CNI + Cilium overlay IPAM (no
Azure network perms needed), the org "no public API endpoint" policy (API
restricted to your current IP), ACR admin-cred pull secret (no role
assignment), the Tetragon gRPC Service, numeric UIDs for `runAsNonRoot`, and
`HOSTNAME=0.0.0.0` for the Next.js frontend. Toggles: `FORCE_REBUILD=true`
recreates the cluster, `SKIP_BUILD=true` reuses existing image tags. When it
finishes, run the port-forwards it prints and open http://localhost:3000.

If your IP later changes and `kubectl` times out, re-add it:
`az aks update -g k8s-goat-eastus-rg -n k8s-goat-cluster --api-server-authorized-ip-ranges "$(curl -s ifconfig.me)/32"`.

---

## Manual path (any cluster: kind / minikube / AKS)

Four files, run in order.

```bash
# 0. Point kubectl at the right cluster
kubectl config current-context     # should be your AKS/Goat cluster

# 1. Inspect — decides mock-vs-live and how to load images
bash deploy/k8s-goat/inspect.sh

# 2. Build the two images and make them reachable by the cluster
bash deploy/k8s-goat/build-and-load.sh
#    kind/minikube: side-loads onto nodes.
#    AKS: prints the ACR push+attach commands (nodes can't be side-loaded).

# 3. Deploy (MOCK mode — works on any cluster, no dependencies)
export IC_IMAGE_PREFIX=            # empty for local; youracr.azurecr.io/ for AKS
export IC_IMAGE_TAG=0.2.0
envsubst < deploy/manifests/isovalent-control.yaml | kubectl apply -f -
kubectl -n isovalent-control rollout status deploy/isovalent-control-backend
kubectl -n isovalent-control rollout status deploy/isovalent-control-frontend

# 4. Open it (no ingress needed)
kubectl -n isovalent-control port-forward svc/isovalent-control-backend  8081:8081 &
kubectl -n isovalent-control port-forward svc/isovalent-control-frontend 3000:3000 &
#    → http://localhost:3000
```

Mock mode gives you the full UI immediately (dashboards, service map, flow +
runtime streams, policy builder) with generated demo data — a good way to
confirm the deploy before wiring real data.

When you're ready for **real flows/policies/runtime events from Goat**, follow
`live-mode.md` — it adds Cilium (if needed), Hubble and Tetragon, then flips one
env var. Note the AKS caveat in that file: Cilium is a cluster-create-time
choice, so live mode on AKS may mean a fresh "Azure CNI Powered by Cilium"
cluster with Goat redeployed onto it.

## Requirements on your workstation

`kubectl`, `docker`, `envsubst` (from gettext), and — for AKS image hosting —
`az`. The scripts run entirely from your machine against your kube-context;
nothing needs to be pushed to a public registry for the local-cluster path.

## Teardown

```bash
kubectl delete ns isovalent-control
kubectl delete clusterrole,clusterrolebinding isovalent-control-policies
```
