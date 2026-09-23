# Quick start

## Run it

```bash
git clone [<this repo>](https://github.com/orcohe-Cisco/Isovalent/tree/main/Isovalent-GUI)
cd isovalent-control
./run.sh
```

Then open <http://localhost:3000>.

`run.sh` will ask before installing anything. Answer no to a prompt and it
carries on without that component — the console starts either way and the
Diagnostics page tells you what is missing.

Useful variants:

```bash
./run.sh --no-goat              # skip the deliberately vulnerable workloads
./run.sh --no-grafana           # skip Prometheus and Grafana
./run.sh --no-agents            # Cilium and Tetragon are already installed
./run.sh --registry ghcr.io/you # remote cluster: push images somewhere it can pull from
./run.sh --skip-build --tag 0.4.0 --registry ghcr.io/you
                                 # deploy an image that's already built and pushed
                                 # (by CI, or by you earlier) — no local Docker needed
./run.sh --local                # spin up a local minikube cluster and deploy onto it — no cloud, no kube-context needed
./run.sh --forward-only         # re-establish the port-forwards after closing the terminal
./run.sh --uninstall            # remove the console (agents and GOAT are left alone)
```

## Pointing kubectl at a cluster

`run.sh` never touches your cloud credentials, and only ever creates a cluster
itself if you ask for one with `--local`. Otherwise, get a context yourself,
whichever way you normally do:

```bash
# Azure
az aks get-credentials --resource-group <resource-group> --name <cluster-name>

# AWS
aws eks update-kubeconfig --region <region> --name <cluster-name>

# Google
gcloud container clusters get-credentials <cluster-name> --region <region>

# Local (kind)
kind create cluster --config deploy/local/kind.yaml

# Local (minikube) — or just run: ./run.sh --local
minikube start --driver=docker --cni=false
```

The console's Deployment page shows the same commands, with your namespace and
endpoints filled in.

## Testing locally with `--local`

If your cloud cluster is unavailable or misbehaving and you just want to
verify the console itself works, `./run.sh --local` sidesteps the cloud
entirely: it starts (or reuses) a minikube cluster in its own profile
(`isovalent-control`, so it won't collide with a minikube cluster you already
use for something else), switches kubectl to it, and runs the exact same
Cilium/Tetragon/Grafana/GOAT/build/deploy steps as any other run — nothing
about the console or its manifests is different on minikube. It needs Docker
running (minikube's `docker` driver) but nothing else; no registry, no
`--registry` flag, no cloud login. Requires `minikube` itself
(`brew install minikube` on macOS). Tear the cluster down with
`minikube delete -p isovalent-control` when you're done with it.

## What you get

| URL | What |
| --- | --- |
| <http://localhost:3000> | The console |
| <http://localhost:8081/healthz> | Backend health and the running build |
| <http://localhost:8081/api/openapi.yaml> | The API contract |
| <http://localhost:1234> | Kubernetes GOAT, if installed |

The service map and the dashboards are embedded *inside* the console. The
direct Hubble UI and Grafana forwards exist so that when something looks wrong
you can tell whether the proxy or the upstream is at fault.

## First five minutes

1. **Overview** — confirm flows and runtime events are arriving. If a number is
   stuck at zero, the health pill at the top right links straight to
   Diagnostics, which says which component is not answering and what to do.
2. **Policy Library** → pick a few runtime policies, apply them in **alert**
   mode. Never start in block mode.
3. Wait. Ten minutes is enough on a busy cluster.
4. **Exclusions** — the policies you applied are now listed with hit counts.
   Open the loudest one. You will usually find one process accounting for most
   of it. Tick it, preview, apply.
5. **Active Policies** — now switch that policy to enforce, having seen what it
   would actually kill.

## Troubleshooting

**`mapfile: command not found`, or a script dies immediately on macOS.**
macOS ships bash 3.2 from 2007. Every script here is written for it, and
`make check-scripts` enforces that in CI. If you hit this, it is a bug —
please report it.

**`UPGRADE FAILED: ... nil pointer evaluating interface {}.enabled` when enabling Hubble.**
Helm's `--reuse-values` reuses the previous release's values and does *not*
merge in the new chart's defaults. Upgrading an old Cilium release with a newer
chart therefore leaves any newly-introduced key nil, and a template that reads
it blows up. `run.sh` now pins the upgrade to the chart version you already
have, and uses `--reset-then-reuse-values` on Helm 3.14+. Your CNI is never
left broken by this — Helm rolls a failed upgrade back.

If it still fails, enable Hubble yourself and skip that step:

```bash
cilium hubble enable --ui
./run.sh --no-agents
```

**The console's own backend/frontend pods never become Ready (`did not become
ready in time`), on a freshly installed cluster.**
Two different bugs have produced exactly this symptom; both are fixed, but
check `kubectl get pods -n isovalent-control` to see which one (if either)
you're still hitting on an older copy of the script:

- `STATUS: ImagePullBackOff` on **`--local` / kind / k3d / Docker Desktop**:
  the manifest's `imagePullPolicy` used to be hardcoded to `Always`. That is
  correct for a real registry (a reused tag must not silently serve a stale
  cached layer), but on a local platform the image is loaded straight into
  the node's cache and was never pushed anywhere — there is no registry to
  pull from. `Always` there made the kubelet ignore the image that was just
  loaded and attempt a real pull for a name that only exists locally, which
  fails outright, on every node, no matter how correctly the build and load
  steps went. `run.sh` now sets `imagePullPolicy: Never` for
  kind/k3d/minikube/Docker Desktop and leaves it `Always` for a real
  registry, via `IC_IMAGE_PULL_POLICY` in the manifest template.
- `STATUS: Pending` or `ContainerCreating` that never clears, on a **brand
  new cluster**: `run.sh` used to wait for Hubble Relay to roll out but never
  for Cilium itself — the CNI — to finish rolling out to every node first.
  On a fresh cluster (very much including a fresh `--local` cluster) the
  console's pods can get scheduled before pod networking actually works.
  `run.sh` now waits for `ds/cilium` (and `ds/cilium-envoy` if the chart
  deployed one) to be Ready before moving on to Tetragon/Grafana/GOAT/the
  console at all.

If neither matches — the pod is `Running` but never `Ready` — that's a
genuine startup problem, not one of the above; check
`kubectl logs -n isovalent-control -l app.kubernetes.io/component=backend`
(note the label — it is `app.kubernetes.io/component`, not `app`).

**The images build but the cluster runs an old version.**
Check `curl localhost:8081/healthz`: it reports the version, commit and build
date of the running binary. A reused image tag plus `imagePullPolicy:
IfNotPresent` will serve a cached layer forever; the shipped manifest uses
`Always` and `run.sh` also does a `rollout restart`.

**`docker push` failed / the pods are stuck in `ImagePullBackOff`.**
A remote cluster cannot see images built on your laptop. If the Azure CLI is
installed, `run.sh` offers the registries on your subscription and runs
`az acr login` for you; otherwise pass one:

```bash
./run.sh --registry <name>.azurecr.io
./run.sh --registry ghcr.io/your-org
```

Log in to it first if the script cannot (`docker login`,
`aws ecr get-login-password | docker login --password-stdin ...`). Note that
this login step now runs whether you pass `--registry` yourself or let the
script discover it — earlier it only ran on the discovery path, so an
explicit `--registry <name>.azurecr.io` with a stale or missing `az acr
login` token used to fail confusingly mid-push instead of being caught first.

`run.sh` also wires up the cluster side automatically: once the push
succeeds, it copies your local `~/.docker/config.json` into a
`kubernetes.io/dockerconfigjson` Secret (`ic-registry-pull`) in the target
namespace and attaches it to the `default` and `isovalent-control-backend`
service accounts, so the nodes can pull without you creating that secret by
hand. If pods still show `ErrImagePull` after that, check that your local
docker login for that registry hasn't expired (`az acr login` tokens are
short-lived) and re-run `run.sh`.

**Why does `run.sh` need Docker at all?**
Only to build the two images from source (`docker build`) and, on a remote
cluster, push them (`docker push`) — that's the "Building images" step.
Every version of this script has needed Docker for that step; if a past run
worked without Docker running, it's because that run never reached the
build step at all — most likely `--forward-only` (re-establishing
port-forwards against pods that were already deployed), or the images for
that tag were already sitting in the registry from an earlier build. If you
have images already built and pushed (by CI, or by an earlier run) and just
want to deploy that tag, use `--skip-build --tag <tag> --registry <registry>`
— it goes straight to `kubectl apply` and never touches Docker.

**`failed to connect to the docker API at unix:///var/run/docker.sock`.**
Docker is installed but its daemon isn't running — almost always Docker
Desktop hasn't been launched (or was closed). `run.sh` now checks `docker
info` before it tries to build and fails with this same explanation instead
of the raw socket error. Start Docker Desktop (macOS/Windows) or `sudo
systemctl start docker` (Linux) and re-run.

**Preflight warns that nodes are `NotReady`.**
The cluster itself is unhealthy, and continuing usually just produces
confusing failures three steps later (pods stuck `Pending`, image pulls
hang, rollouts time out). On AKS, this is very often one specific failure —
a stuck node pool operation, or a node that never finished bootstrapping
(commonly an instance-metadata-service timeout) — with a known fix: abort
the stuck operation, then cycle the pool so it provisions fresh VMs.

If `run.sh` detects it's talking to AKS and the `az` CLI is available, it now
offers to run that fix itself (`az aks nodepool operation-abort` followed by
scaling the pool down and back up), then waits up to two minutes for the
nodes to come back before continuing. Say no, or if it's a different
provider, and you'll get the same guidance to do it yourself — from your
cloud console or CLI (EKS/GKE included).

It only calls `operation-abort` when the pool's `provisioningState` is
`Failed` — a normal in-progress state like `Scaling`, `Updating`, or
`Creating` is left alone and just waited on, rather than aborted. That
matters if you interrupt a run partway through this fix (Ctrl-C/Ctrl-Z) and
re-run `run.sh`: aborting a healthy in-progress operation to start a new one
is how a real fix turns into an actually broken node pool. If you ever see
`az aks nodepool list -o table` report anything other than `Succeeded`, let
it finish before re-running `run.sh` rather than interrupting it again.

**A node is `NotReady` and `kubectl describe node <name>` shows `container
runtime is down` / `containerd service is not running`.**
This is a different failure than a stuck node-pool operation — one specific
VM's containerd crashed, and no amount of scaling the pool touches that.
`run.sh` now checks for this first: it reads the node's own `providerID`
(which already spells out the exact resource group, VMSS, and instance
number), and offers to `az vmss reimage` just that instance — the same fix
you'd reach for by hand, without cycling the rest of the pool. It only falls
back to the broader pool-wide fix if no node shows this specific symptom.

**Flows are empty.**
Hubble Relay is not reachable. `kubectl -n kube-system get deploy hubble-relay`.
Diagnostics reports this explicitly, including the address it tried.

**Runtime events are empty.**
Tetragon is not installed, or its gRPC listener is bound to `localhost`
inside the pod instead of all interfaces — a chart-default that leaves every
*other* pod, including this console, getting "connection refused" even
though Tetragon itself is healthy. `run.sh` now sets
`tetragon.grpc.address=0.0.0.0:54321` on install, checks an existing
installation's configured address on every run and offers to fix it in
place, and (re)creates a `tetragon` Service in `kube-system` built directly
from the DaemonSet's own pod selector — so the console's `IC_TETRAGON_ADDR`
has a stable DNS name to resolve regardless of what the chart calls its own
Service. Separately, `tetragon.exportAllowList` filters events at the agent
— if it is set to something narrow, the console sees only what gets past it.

**The user column in Exclusions is empty.**
Install Tetragon with `--set tetragon.enableProcessCred=true`. Without it the
agent does not report process credentials at all.

**The Service Map or Dashboards panel is blank.**
Both are proxied through the backend at `/hubble-ui/` and `/grafana/`. Open
Diagnostics: it probes the same addresses and reports the HTTP status it got.

**A policy applies fine but never fires.**
Very common, and not a console bug. Check the hook name and the pod selector.
A `matchArgs` `NotPrefix` on the wrong argument index matches nothing, silently
— which is why the Exclusions tab takes the index from the observed hit rather
than guessing.

**"policy targets a protected namespace".**
Working as intended. Policies written here cannot select the console's own
components. Extend or narrow the set with `IC_PROTECTED_NAMESPACES`.

## Running the backend outside the cluster

```bash
kubectl proxy &                      # the backend falls back to 127.0.0.1:8001
kubectl -n kube-system port-forward svc/hubble-relay 4245:80 &
kubectl -n kube-system port-forward svc/tetragon 54321:54321 &

cd backend && go run ./cmd/server
cd frontend && npm install && npm run dev
```

`IC_HUBBLE_RELAY_ADDR` and `IC_TETRAGON_ADDR` default to those local ports.
