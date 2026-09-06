# Isovalent Control

A single console for Cilium, Hubble and Tetragon: see what the cluster is
doing, write the policy that governs it, and then — the part every other tool
skips — find out why the policy fired and quieten it without turning it off.

Everything it shows comes from a live cluster. There is no demo mode, no
generated data, and no code path that can produce a flow that did not happen.
That constraint is deliberate: a console that can invent a drop is a console you
cannot trust when it reports one.

```bash
./run.sh
```

That is the whole quick start. It checks your kube-context, installs Cilium,
Hubble and Tetragon if they are missing, optionally adds Grafana and Kubernetes
GOAT, builds and deploys the console, and sets up every port-forward. It works
against AKS, EKS, GKE, kind, k3d, minikube or any other Kubernetes — the only
cloud-specific step is getting credentials, which you do yourself.

---

## What it does

**Monitor** — the overview, live flows, live runtime events, and two embedded
consoles rather than reimplementations: the real **Hubble UI** as the service
map, and **Grafana** for dashboards. Both are proxied through the backend, so
they share this console's origin and need no second login.

**Protect** — author CiliumNetworkPolicy, CiliumClusterwideNetworkPolicy,
native NetworkPolicy and Tetragon TracingPolicy from one page. It loads what
the cluster already has, including policies this console did not write. Nothing
applies until it has been dry-run: network policies against recent flows,
runtime policies against stored history. Runtime enforcement has no undo.

**Exclusions** — the reason this project exists. Every runtime policy, how many
times it has fired, and what fired it, broken down by process, parent process,
file path, user, pod label and container. Tick the values that are legitimate,
preview the change, and the exclusion is written into the live policy using the
mechanism Tetragon actually supports for that dimension. Counters reset, so the
next few minutes tell you whether it worked.

**History** — one search across both data sources. Timeline, free text, a
window from five minutes to thirty days, and clickable facets built from the
result set, so you cannot filter on a namespace that does not exist and
conclude nothing happened. Filter to blocked traffic, then ask *why*: the
drill-down names the policy and the specific clause inside it that matched —
the `matchBinaries` entry, the `matchArgs` index, the ingress rule.

**Assistant** — point it at Anthropic, Gemini, or any OpenAI-compatible
endpoint. It reads the posture and proposes changes; it cannot apply anything.
Approvals go through the same audited endpoints a human click uses, so an
AI-proposed change gets identical validation and self-protection.

**Operate** — alert routing to Slack, Teams, Webex, generic webhooks, syslog
(RFC5424 or CEF), Splunk HEC, Elasticsearch and Microsoft Sentinel; an audit
log of every change with before/after diffs; a diagnostics page that probes
every dependency and tells you which leg is broken; API tokens and the full
reference; and generated deployment commands for whichever platform you are on.

## Self-protection

Policies authored in this console cannot target the console.

That is not a UI convention, it is enforced in the backend: a manifest that
selects a protected namespace is rejected, and every TracingPolicy written here
gets a `NotIn` exemption for the console's own pods injected automatically. The
failure it prevents is specific — you build a cluster-wide `Sigkill` policy,
apply it, and the first thing it kills is the process that would have let you
undo it.

`GET /api/v1/config` lists the protected namespaces; `IC_PROTECTED_NAMESPACES`
extends the set.

## Requirements

- Kubernetes 1.24+, and `kubectl` pointed at it
- Helm 3
- Docker, unless you build and push the images another way
- Cilium with Hubble Relay, and Tetragon — `run.sh` installs both if absent

Tetragon's enforcement hooks (`Sigkill` on exec) need BPF-LSM. Observation
hooks such as `security_file_open` do not, and work on considerably more
kernels. The policy library says which is which.

## Layout

```
backend/     Go API. Vendored dependencies — the image builds with no network.
frontend/    Next.js console.
deploy/      Kubernetes manifests, Grafana dashboards, a kind config.
charts/      Helm chart.
policies/    Ready-to-apply Tetragon policies.
gen/         Generator for the policy library from a cilium/tetragon checkout.
hack/        Repo tooling, including the bash 3.2 check.
docs/        Architecture, deployment, API and operations guides.
goat-isovalent-handbook/
             Kubernetes GOAT scenario walkthroughs paired with the specific
             Isovalent Control policy that detects or blocks each one —
             useful for demoing Exclusions and Protect against real findings
             instead of synthetic ones.
run.sh       Install, deploy and port-forward everything.
```

## Building by hand

```bash
make build           # go build + next build
make test            # go vet + go test
make check-scripts   # verify the shell scripts still run on macOS bash 3.2
make docker          # build both images locally
```

The backend vendors its dependencies (`backend/vendor`), so `docker build`
needs no module downloads at all — no `GOPROXY`, no `go.sum` reconciliation,
and no `go mod tidy` inside the image. That last one is not an aesthetic
preference: `go mod tidy` in a Dockerfile re-resolves the module graph against
whatever network the build agent happens to have, and it is the single most
common way this image used to break.

## Documentation

- [QUICKSTART.md](QUICKSTART.md) — the five-minute version, plus troubleshooting
- [docs/deployment.md](docs/deployment.md) — platforms, configuration, production notes
- [docs/architecture.md](docs/architecture.md) — how the pieces fit and why
- [docs/api.md](docs/api.md) — automating the console
- [docs/exclusions.md](docs/exclusions.md) — tuning a noisy policy properly
- [CONTRIBUTING.md](CONTRIBUTING.md)

## Licence

Apache 2.0. See [LICENSE](LICENSE).
