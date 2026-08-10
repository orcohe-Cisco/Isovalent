# Isovalent-Control

**A unified open-source GUI management platform for the Isovalent / Cilium stack** — observe, visualize, build policies for, and monitor **Cilium**, **Hubble**, and **Tetragon** from a single multi-tenant web interface.

---

## 🚀 Install in one command (any Kubernetes)

Works on **kind, minikube, k3s, Docker Desktop, EKS, AKS, and GKE**. The default
install runs in **demo mode** — no Cilium/Hubble/Tetragon required — so you see
the full UI immediately on any cluster.

```bash
git clone https://github.com/orcohe-Cisco/Isovalent.git
cd Isovalent/Isovalent-GUI
./install.sh
```

That's it — it deploys the app against your current `kubectl` context and opens
`http://localhost:3000`.

Coming back later, or opening a new terminal? Run **`./connect.sh`** — it
restarts the port-forwards and verifies each one. (See
[Reconnecting](#reconnecting--connectsh).)

> **Private repo?** If `git clone` asks for a username/password, the repository
> isn't public — GitHub reports "Repository not found" instead of "access
> denied". Password auth was removed by GitHub in 2021, so authenticate first
> with `gh auth login` (then re-run the clone), use an SSH remote
> (`git@github.com:owner/repo.git`), or make the repo public.

> **Prefer double-click?** On macOS, double-click **`install.command`** in Finder
> (right-click → Open the first time). On Windows, run `./install.sh` from Git Bash or WSL.

**Local cluster with no registry?** Build the images locally and load them:

```bash
./install.sh --build
```

(`--build` builds the images locally and side-loads them into kind/minikube, so
no registry is involved.)

**Want real data** (live flows, policies, runtime events)? You need Cilium +
Hubble + Tetragon in the cluster. Let the installer set them up and turn on
dashboards:

```bash
./install.sh --live --install-stack --with-monitoring
```

| Flag | What it does |
|---|---|
| _(none)_ | demo mode, public images — runs on any cluster |
| `--build` | build images locally and load into kind/minikube (no registry) |
| `--live` | wire to real Hubble/Tetragon/K8s (requires the stack) |
| `--install-stack` | install Cilium + Hubble + Tetragon (best-effort per platform) |
| `--with-monitoring` | install Prometheus + Grafana + the dashboard |
| `--with-goat` | deploy **Kubernetes Goat** (opt-in) — cloned to `./_goat/` |
| `--image-repo` / `--tag` | use your own registry / tag |
| `--uninstall` | remove everything the installer created |

**Kubernetes Goat is not installed by default.** It's an intentionally
vulnerable demo target; add `--with-goat` (or `WITH_GOAT=true` for the AKS
script) and it's pulled from [its own GitHub](https://github.com/madhuakula/kubernetes-goat)
into a separate `./_goat/` folder, never mixed into the platform.

> **Public images:** the default install pulls
> `ghcr.io/<owner>/isovalent-control-{backend,frontend}`. Push once via the
> included GitHub Actions workflow (`.github/workflows/ci.yaml`) and make the
> two packages **public** in your repo's Packages settings — or use `--build`
> for a fully offline local install, or `--image-repo` to point at your registry.

### Reconnecting — `./connect.sh`

A port-forward is a process on **your** machine: it dies when you reboot, sleep
the laptop, drop the VPN, or close the terminal that started it. Run this from
any shell, any time:

```bash
./connect.sh
```

It verifies the cluster is reachable, waits for pods to be ready, clears stale
forwards, picks free local ports, starts fully detached supervisors that
reconnect on their own, then **probes each port and reports which ones actually
answered** — a live `kubectl` process proves nothing, since it survives the pod
behind it being replaced. `./connect.sh status` and `./connect.sh stop` do the
obvious.

The forwards are detached with `nohup` + `disown`, so **Ctrl-Z, closing the tab,
or exiting the shell will not kill them**. (Ctrl-Z is a common trap: it suspends
the forward rather than stopping it, so the port stays bound but nothing is
forwarded.)

**The app works on any local port.** The frontend container runs a small
reverse proxy ([`frontend/proxy.js`](frontend/proxy.js)) that serves the UI and
forwards `/api`, `/ws` and `/healthz` to the backend Service inside the cluster.
The browser therefore only ever talks to the origin that served the page, and no
backend URL is compiled into the bundle. One forward, on whatever port is free:

```bash
kubectl -n isovalent-control port-forward svc/isovalent-control-frontend 3000:3000
```

Earlier builds inlined `http://localhost:8081` into the browser bundle at build
time, so forwarding the backend to any other port produced a perfectly rendered
UI with zero data and no visible error. That failure mode is gone.

Set `NEXT_PUBLIC_API_URL` at build time only if you terminate the API on a
different host (an ingress split across two domains).

### Using the original Hubble UI and Grafana inside the app

The Service Map tab embeds the **official Hubble UI** (full L3–L7 map with
Hubble's own filtering and drill-down) and the Dashboards tab embeds
**Grafana**. `./install.sh --install-stack --with-monitoring` enables and wires
both automatically, and `./connect.sh` opens the port-forwards they need
(12000 for Hubble UI, 3001 for Grafana).

To point them elsewhere (an ingress, a shared Grafana):

```bash
kubectl -n isovalent-control set env deploy/isovalent-control-backend \
  IC_HUBBLE_UI_URL=https://hubble.example.com IC_GRAFANA_URL=https://grafana.example.com
```

If `IC_HUBBLE_UI_URL` isn't set, the Service Map falls back to the built-in map.

### Durable event retention

The enforcement log keeps **14 days** by default (`IC_RETENTION_DAYS`). In-memory
storage is the default; for retention that survives restarts, point it at Postgres:

```bash
kubectl -n isovalent-control set env deploy/isovalent-control-backend \
  IC_DB_DSN='postgres://user:pass@postgres:5432/isovalent?sslmode=disable'
```

Cloud-specific, fully-automated setups live in [`deploy/`](deploy/):
[`deploy/k8s-goat/rebuild-aks.sh`](deploy/k8s-goat/) provisions a whole AKS
cluster end-to-end; the manual per-cluster path is documented there too.

---

## Architecture Overview

```mermaid
flowchart TB
    subgraph Browser["Web UI (Next.js + React Flow + Tailwind)"]
        DASH["Dashboards\nHealth / Security / Network / Runtime"]
        SMAP["Service Map\n(React Flow)"]
        PB["Policy Builder\nCNP / CCNP / TracingPolicy\n(form ⇄ YAML sync)"]
        FLOWS["Live Flow Viewer\n(Hubble)"]
        EVENTS["Runtime Event Stream\n(Tetragon)"]
    end

    subgraph Backend["isovalent-control API server (Go)"]
        REST["REST API\n(chi router)"]
        WS["WebSocket Hub\n(flows / events / alerts)"]
        AUTHZ["AuthN/AuthZ\nOIDC · JWT · RBAC scopes\n(org → cluster → namespace)"]
        POL["Policy Service\n(dynamic K8s client)\nCNP · CCNP · TracingPolicy"]
        HUB["Hubble Source\n(gRPC Relay client | mock)"]
        TET["Tetragon Source\n(gRPC client | mock)"]
        ALERT["Alert Engine\n(rules → routes)"]
    end

    subgraph Cluster["Kubernetes Cluster(s)"]
        K8S["kube-apiserver\n(CRDs: CiliumNetworkPolicy,\nCiliumClusterwideNetworkPolicy,\nTracingPolicy)"]
        RELAY["Hubble Relay\n:4245 gRPC"]
        TGRPC["Tetragon gRPC\n:54321"]
    end

    subgraph Stores["State"]
        PG[("PostgreSQL\nhistorical alerts/events")]
        REDIS[("Redis\npub/sub · sessions")]
    end

    Browser -- "HTTPS / JSON" --> REST
    Browser -- "WSS (live streams)" --> WS
    REST --> AUTHZ
    WS --> AUTHZ
    AUTHZ --> POL & HUB & TET & ALERT
    POL -- "client-go (dynamic)" --> K8S
    HUB -- "gRPC GetFlows" --> RELAY
    TET -- "gRPC GetEvents" --> TGRPC
    ALERT --> PG
    WS --> REDIS
    ALERT -- "Slack / PagerDuty / Webhook / SIEM" --> EXT["External Sinks"]
```

## Repository Layout

```
isovalent-control/
├── backend/                     # Go API server
│   ├── cmd/server/              # main entrypoint
│   ├── internal/
│   │   ├── config/              # env/file configuration
│   │   ├── server/              # HTTP server, routes, REST handlers
│   │   ├── auth/                # OIDC/JWT verification + RBAC middleware
│   │   ├── k8s/                 # dynamic client wrappers for Cilium/Tetragon CRDs
│   │   ├── hubble/              # Hubble flow source (live gRPC | mock)
│   │   ├── tetragon/            # Tetragon event source (live gRPC | mock)
│   │   ├── stream/              # WebSocket hub / broadcast fan-out
│   │   └── mock/                # demo-mode data generators
│   ├── api/openapi.yaml         # REST API contract
│   └── Dockerfile               # distroless build
├── frontend/                    # Next.js (App Router) + Tailwind + React Flow
│   └── src/
│       ├── app/                 # routes: /, /network, /runtime, /policies, /flows
│       ├── components/          # ServiceMap, PolicyBuilder, FlowTable, charts…
│       └── lib/                 # API client, WebSocket hook, YAML sync
├── charts/isovalent-control/    # Helm chart
├── deploy/docker-compose.yaml   # one-command local demo
├── hack/                        # proto refresh + restricted-network helpers
├── .github/workflows/           # CI (Go build/test, Next build, image publish)
├── docs/                        # architecture & design docs
└── Makefile
```

## Quick Start (demo mode — no cluster required)

```bash
# Terminal 1 — backend (mock data sources enabled by default)
cd backend
go mod tidy   # first run only — generates go.sum
go run ./cmd/server

# Terminal 2 — frontend
cd frontend
npm install
npm run dev
# open http://localhost:3000
# In `npm run dev` there is no proxy in front, so the client talks to the
# backend on :8081 directly. In the container it is same-origin.
```

Demo mode simulates a realistic microservices topology (Hubble L4/L7 flows,
drops, DNS/HTTP traffic) plus Tetragon process/syscall/file events including
`SIGKILL` enforcement actions — so every screen is populated out of the box.

## Connecting to a real cluster

```bash
export IC_MODE=live
export IC_KUBECONFIG=$HOME/.kube/config
export IC_HUBBLE_RELAY_ADDR=hubble-relay.kube-system.svc:80   # or port-forward localhost:4245
export IC_TETRAGON_ADDR=tetragon.kube-system.svc:54321
go run ./cmd/server
```

| Env var | Default | Description |
|---|---|---|
| `IC_MODE` | `mock` | `mock` or `live` |
| `IC_LISTEN_ADDR` | `:8081` | API listen address |
| `IC_KUBECONFIG` | in-cluster → `$KUBECONFIG` | path to kubeconfig |
| `IC_HUBBLE_RELAY_ADDR` | `localhost:4245` | Hubble Relay gRPC endpoint |
| `IC_TETRAGON_ADDR` | `localhost:54321` | Tetragon gRPC endpoint |
| `IC_OIDC_ISSUER` | _(empty = auth disabled, dev mode)_ | OIDC issuer URL (Okta/Keycloak/Dex/Azure AD) |
| `IC_OIDC_CLIENT_ID` | | expected `aud` claim |
| `IC_DEV_ADMIN` | `true` when auth disabled | injects an admin identity for local dev |

## Feature Matrix (Phase 1)

| Module | Status |
|---|---|
| Multi-tenant RBAC model (org → cluster → namespace scopes) | ✅ middleware + claims mapping |
| OIDC/JWT authentication | ✅ (any OIDC issuer via JWKS) |
| CNP / CCNP / TracingPolicy list, get, apply, delete | ✅ dynamic client |
| Policy Builder with bi-directional YAML sync | ✅ |
| Hubble live flow stream → WebSocket | ✅ (live + mock) |
| Tetragon event stream → WebSocket | ✅ (live + mock) |
| Service dependency map | ✅ aggregated from flows |
| Golden-signals dashboard (drops, error rate, throughput) | ✅ |
| Enforcement visibility (drops + SIGKILL alerts) | ✅ |
| **Tetragon runtime policies — organized + one-click Monitor/Kill/Remove** | ✅ (v0.2) |
| **Suggested best-practice Tetragon TracingPolicies** | ✅ `policies/tetragon/` |
| **Embedded original Hubble UI** (Service Map) | ✅ `IC_HUBBLE_UI_URL` |
| **Embedded Grafana + official Cilium/Hubble dashboards** | ✅ `IC_GRAFANA_URL` |
| **Enforcement log — blocked/killed/monitored, by engine, with rule + event** | ✅ 14-day retention |
| **L7 deep investigation — HTTP header capture + search** | ✅ (v0.2) |
| **Prometheus + Grafana observability bundle** | ✅ `deploy/observability/` |
| **Alert routing (Slack / PagerDuty / Webhook / Splunk-SIEM) + dedup** | ✅ (v0.2) |
| **Dry-run: simulate a network policy against live flows** | ✅ (v0.2) |
| **GitOps PR apply mode (GitHub)** | ✅ (v0.2) |
| **Historical store (in-memory / Postgres) + time-travel UI** | ✅ (v0.2) |
| **Prometheus `/metrics` endpoint on the backend** | ✅ (v0.2) |

## Building behind a restricted network

If your environment blocks `proxy.golang.org` and the Go vanity-import hosts
but allows `github.com`, run `hack/offline-replaces.sh apply` to pin every
dependency to its GitHub mirror, then build with
`GOPROXY=direct GOSUMDB=off GOFLAGS=-mod=mod go build ./...`.
Run `hack/offline-replaces.sh drop` to restore the canonical `go.mod`.

## License

Apache-2.0 — see [LICENSE](LICENSE). Cilium®, Hubble™ and Tetragon™ are
projects of the CNCF; this is an independent community UI and is not
affiliated with or endorsed by Isovalent or the CNCF.
