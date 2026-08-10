# Isovalent-Control — Architecture (Phase 1)

## Design goals

1. **Single pane of glass** for Cilium (network policy), Hubble (flows) and
   Tetragon (runtime security) across many clusters and tenants.
2. **Zero-cluster demo mode**: every screen must work with simulated data so
   the project is evaluable in 60 seconds (`IC_MODE=mock`, the default).
3. **Thin, honest integration layer**: we talk to the *native* APIs — the
   Kubernetes API for CRDs (via the dynamic client, so no version-coupled
   vendoring of Cilium types), Hubble Relay gRPC for flows, Tetragon gRPC for
   runtime events. No agents, no sidecars.

## Data plane → UI pipeline

```
Hubble Relay ──gRPC GetFlows──┐
                              ├─→ Source (chan) ─→ Aggregator ─→ ring buffers, service-map graph,
Tetragon ────gRPC GetEvents───┘                        │           golden-signal counters
                                                       └─→ Stream Hub ─→ WebSocket topics:
                                                                          /ws/flows /ws/events
```

- **Sources** normalize native protos into small JSON-friendly structs
  (`hubble.Flow`, `tetragon.Event`) shared by the mock generators and the live
  clients — the frontend cannot tell the difference.
- The **Aggregator** maintains: a bounded recent-flows/events buffer, a
  service-dependency edge map with per-edge verdict counters, and rolling
  golden-signal counters (throughput, drop rate, HTTP 5xx rate, DNS failures,
  Tetragon kills).
- The **Stream Hub** is a topic-based fan-out; slow consumers are dropped
  rather than allowed to backpressure the pipeline.

## Multi-tenancy & RBAC model

```
Organization ─→ Cluster ─→ Namespace ─→ (Team via OIDC group claim)
```

Identity arrives as an OIDC JWT. The `groups`/`roles` claims are mapped to
scoped roles:

| Role | Capabilities |
|---|---|
| `viewer` | read dashboards, flows, events, policies |
| `editor` | + create/update policies in scoped namespaces (draft + apply) |
| `admin`  | + cluster-wide policies (CCNP, TracingPolicy), delete, settings |

Scopes restrict roles to namespace globs (e.g. `editor:team-payments/*`).
Enforcement happens in middleware (`internal/auth`) before any handler runs;
namespace-scoped list responses are filtered server-side.

With `IC_OIDC_ISSUER` unset the server runs in dev mode and injects a
synthetic admin identity — never deploy that outside a laptop.

## Policy lifecycle

1. UI builder edits a structured model ⇄ YAML (bi-directional sync client-side).
2. `POST /api/v1/policies/*` submits raw YAML; the backend validates GVK
   against an allowlist (CNP, CCNP, TracingPolicy, TracingPolicyNamespaced)
   and performs a server-side-apply via the dynamic client.
3. Phase 2 adds dry-run-against-live-flows simulation and a GitOps PR mode
   (render → branch → PR) as an alternative to direct apply.

## Why the dynamic client instead of Cilium's typed clientset?

Vendoring `github.com/cilium/cilium` pins us to one Cilium minor version and
inflates the module graph enormously. The dynamic client + unstructured
objects keeps us forward/backward compatible across Cilium versions at the
cost of compile-time typing — an acceptable trade for a management plane that
mostly round-trips YAML. The Hubble/Tetragon **protos**, by contrast, are
consumed through their published API modules, which are small and stable.
