# Deployment

The console makes no assumptions about where the cluster lives. The only
platform-specific step is obtaining a kube-context, and you do that yourself
with your own credentials — nothing here reads, stores or prints them.

## The short version

```bash
./run.sh
```

## What run.sh actually does

1. **Preflight.** Checks `kubectl` and `helm`, resolves the current context,
   confirms the cluster answers, and detects the platform. Platform detection
   affects exactly one thing: how images reach the nodes.
2. **Cilium + Hubble.** If Cilium is missing, offers to install it with Hubble,
   Relay, UI and the OpenMetrics exporters. If it is already installed via
   Helm, upgrades in place with `--reuse-values` to turn Hubble on. If it is
   installed some other way, leaves it alone and says so.
3. **Tetragon.** Installs with `enableProcessCred` and `enableProcessNs` on,
   and an empty `exportAllowList`. Those three settings are what make the
   Exclusions tab useful rather than half-populated.
4. **Prometheus + Grafana** (optional). Installs kube-prometheus-stack with
   anonymous viewer access and sub-path serving so the console can embed it,
   then loads the shipped dashboards as labelled ConfigMaps.
5. **Kubernetes GOAT** (optional). Clones upstream into `~/.cache` and runs its
   own setup script.
6. **Images.** Builds both, then gets them to the nodes the right way for the
   platform: `kind load`, `k3d image import`, `minikube image load`, nothing at
   all for Docker Desktop, or `docker push` to `--registry` for a remote
   cluster. A remote cluster with no registry is a hard error with instructions
   rather than a silent `ImagePullBackOff` later.
7. **Deploy.** Renders the manifest and applies it, then does a `rollout
   restart` — reused tags plus a node image cache is the most common cause of
   "the new build is broken".
8. **Port-forwards.** Console, API, Hubble UI, Grafana and GOAT. Each one
   checks the port first and says what to do if it is taken.

## Platform notes

**AKS.** Bring your own cluster. Cilium can run in overlay mode alongside the
Azure CNI or replace it; the console does not care which. `az aks
get-credentials --resource-group <rg> --name <cluster>`.

**EKS.** Cilium replaces the VPC CNI in kube-proxy-replacement mode. Read the
Cilium EKS guide before changing an existing cluster — this is not a change to
make casually on something that is serving traffic.

**GKE.** Use a Dataplane V2 cluster, or a standard cluster with the default CNI
removed.

**Local (kind / k3d / minikube).** `deploy/local/kind.yaml` creates a
three-node cluster with the default CNI disabled, ready for Cilium. Tetragon
needs a kernel with BTF. Observation hooks work on Docker Desktop; the
`Sigkill` enforcement paths need BPF-LSM, which that kernel does not always
have.

**OpenShift / restricted PSA.** Both deployments already run as non-root with
`readOnlyRootFilesystem`, dropped capabilities and `RuntimeDefault` seccomp.

## Configuration

Everything is an `IC_*` environment variable on the backend. The Deployment
page in the console renders this table with your own values filled in.

### Data sources

| Variable | Default | Notes |
| --- | --- | --- |
| `IC_HUBBLE_RELAY_ADDR` | `localhost:4245` | gRPC. In-cluster: `hubble-relay.kube-system.svc:80`. |
| `IC_TETRAGON_ADDR` | `localhost:54321` | gRPC. In-cluster: `tetragon.kube-system.svc:54321`. |
| `IC_K8S_API_SERVER` | in-cluster, then `http://127.0.0.1:8001` | Falls back to `kubectl proxy` for local development. |
| `IC_CLUSTER_NAME` | `current-context` | Shown in the sidebar and in every alert. |

### Embedded consoles

| Variable | Default |
| --- | --- |
| `IC_HUBBLE_UI_URL` | `http://hubble-ui.kube-system.svc.cluster.local:80` |
| `IC_GRAFANA_URL` | `http://kube-prometheus-stack-grafana.monitoring.svc.cluster.local:80` |
| `IC_GRAFANA_DASHBOARD_UID` | `isovalent-control` |

Both are reverse-proxied at `/hubble-ui/` and `/grafana/`. The proxy strips
`X-Frame-Options` and the `frame-ancestors` directive, because we are framing
them deliberately from the same origin. It cannot make an unreachable service
reachable — Diagnostics probes the same URLs and reports what came back.

### Self-protection

| Variable | Default |
| --- | --- |
| `IC_SELF_NAMESPACE` | `isovalent-control` |
| `IC_PROTECTED_NAMESPACES` | `kube-system` (the self namespace is always added) |

A manifest that selects a protected namespace is rejected with 403, and every
TracingPolicy authored here gets a `NotIn` exemption on
`app.kubernetes.io/part-of: isovalent-control` injected before it is applied.
Both checks are server-side, so they also apply to API clients and to
AI-proposed changes.

### History and audit

| Variable | Default | Notes |
| --- | --- | --- |
| `IC_DB_DSN` | unset | Postgres. Without it, history and the audit log are in-memory rings lost on restart. |
| `IC_HISTORY_LIMIT` | `20000` | Records per kind in the in-memory ring. |

Set the DSN for anything you intend to investigate later. The in-memory default
is honest but small: it exists so the console works out of the box, not so you
can run an incident review off it.

### Authentication

| Variable | Notes |
| --- | --- |
| `IC_OIDC_ISSUER` | Empty disables authentication entirely. |
| `IC_OIDC_CLIENT_ID` | Expected audience. |
| `IC_OIDC_ROLES_CLAIM` | Default `groups`. |
| `IC_API_TOKENS` | Static machine tokens, `name:role:token`, comma-separated. |

Roles arrive as claim strings: `ic:viewer`, `ic:editor:shop,web` (globs
supported), `ic:admin`.

**With no issuer configured every caller is an admin.** The backend logs a
warning at startup saying exactly that. It is fine for a lab and unacceptable
anywhere else.

### AI assistant

| Variable | Notes |
| --- | --- |
| `IC_AI_PROVIDER` | `anthropic`, `gemini`, `openai`, or `disabled`. |
| `IC_AI_BASE_URL` | For OpenAI-compatible gateways: vLLM, Ollama, LiteLLM, Azure OpenAI. |
| `IC_AI_MODEL` | Sensible default per provider. |
| `IC_AI_API_KEY_FILE` | Path to a mounted Secret. Preferred over `IC_AI_API_KEY`. |

The key never leaves the backend. `/api/v1/ai/status` reports only whether one
is present. The model returns structured suggestions; it cannot apply anything,
and the apply endpoint deliberately does not accept a manifest from it.

### GitOps

| Variable | Notes |
| --- | --- |
| `IC_GITHUB_REPO` | `owner/repo`. |
| `IC_GITHUB_TOKEN` | Needs `contents: write` and `pull_requests: write`. |
| `IC_GITHUB_BASE` | Default `main`. |
| `IC_GITHUB_PATH` | Directory for rendered policies. |

With both repo and token set, apply operations can open a pull request instead
of writing to the cluster. The console shows the choice; ArgoCD or Flux does
the rest.

## Production checklist

- [ ] `IC_OIDC_ISSUER` set. Without it there is no authentication at all.
- [ ] `IC_DB_DSN` set, so history and the audit log survive a restart.
- [ ] Frontend rebuilt with `NEXT_PUBLIC_API_URL` pointing at your real API
      origin. It is baked in at build time, not read at runtime.
- [ ] `IC_CORS_ORIGIN` narrowed from `*`.
- [ ] Grafana behind your own auth rather than the anonymous-viewer default
      `run.sh` uses for a lab.
- [ ] `IC_PROTECTED_NAMESPACES` extended to cover anything else that must never
      be a policy target.
- [ ] Kubernetes GOAT **not** installed. It is deliberately vulnerable.

## Uninstalling

```bash
./run.sh --uninstall
```

Removes the console, its ClusterRole and binding, and — if you installed it
this way — kube-prometheus-stack. Cilium, Tetragon and GOAT are left alone,
because removing a CNI out from under a running cluster is not something a
script should decide to do for you.
