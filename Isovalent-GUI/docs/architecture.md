# Architecture

## Design constraints

Three decisions shape everything else.

**One data path.** Every screen reads from a live cluster. There is no demo
mode and no generated data — the code that could produce it has been removed,
and the only in-memory policy store left is a test double in `internal/k8stest`
that the server binary never links. A console that can invent a dropped flow is
one you cannot trust when it reports a real one.

**Embed, do not reimplement.** The service map is the actual Hubble UI and the
dashboards are actual Grafana, both reverse-proxied through the backend.
Rebuilding either would produce a worse version of a tool people already know,
and it would drift the moment upstream changed. The console's contribution is
putting them one click from the policy that governs what they show.

**The dangerous operation gets the most machinery.** Runtime enforcement kills
processes and has no undo. So it is the one thing that cannot be applied
without a dry run, cannot select the console's own namespace, and cannot be
changed by the AI assistant without a human approving it through the same
audited endpoint a click uses.

## Shape

```
Hubble Relay ──gRPC GetFlows──┐
                              ├─→ normalising Source ─→ Aggregator ─┬─→ ring buffers
Tetragon ────gRPC GetEvents───┘                                     ├─→ service-map graph
                                                                    ├─→ golden-signal buckets
                                                                    ├─→ hit tracker (per policy)
                                                                    ├─→ history store
                                                                    ├─→ alert router → sinks
                                                                    └─→ stream hub → WebSockets

Kubernetes API ──REST──→ policy store ──→ guard ──→ audit ──→ handlers
Hubble UI, Grafana ──reverse proxy──→ /hubble-ui/, /grafana/
```

## Backend packages

| Package | Responsibility |
| --- | --- |
| `hubble`, `tetragon` | gRPC clients that normalise native protos into small JSON-friendly structs. Reconnect with backoff; drop rather than backpressure the agent. |
| `server` | HTTP routing, handlers, the aggregator, the observed-value catalogue. |
| `k8s` | A deliberately minimal REST client, plus policy CRUD, manifest validation, exclusion injection and match explanation. |
| `hits` | Per-policy hit accounting, broken down along the dimensions Tetragon can express an exclusion on. |
| `investigate` | Normalises flows and events onto one row type so a single query spans both. |
| `guard` | Self-protection: rejects manifests targeting protected namespaces, injects the self-exemption. |
| `policy` | Dry-run simulators for network and runtime policies. |
| `audit` | Append-only record of every change, with field-level diffs. |
| `logbuf` | Ring of recent log lines from both backend and browser, plus the slog handler that captures them. |
| `alerts` | Sink fan-out with severity, kind and namespace filtering, and per-sink payload rendering. |
| `ai` | Provider-agnostic LLM client. Suggests; never applies. |
| `proxy` | Reverse proxy for the embedded consoles, stripping frame-blocking headers. |
| `apikeys`, `auth` | Machine tokens and OIDC verification. |
| `store` | History persistence: in-memory ring, or Postgres. |
| `stream` | Topic-based WebSocket fan-out. |

### Why a hand-written Kubernetes client

The CRDs the console manages are round-tripped as opaque JSON manifests. It
never needs typed Cilium or Tetragon structs, so `client-go`'s dynamic-client
machinery would add nothing while multiplying the module graph roughly
fortyfold. The client is about 150 lines and supports the three credential
sources that matter: explicit configuration, in-cluster service account, and
`kubectl proxy` for local development.

Applies are server-side applies (`PATCH` with `application/apply-patch+yaml`,
`fieldManager=isovalent-control`), so the console's ownership of fields is
explicit and a hand-edited policy is not silently clobbered.

### Vendored dependencies

`backend/vendor` is committed and the image builds with `-mod=vendor` and
`GOPROXY=off`. There are no module downloads during a container build at all.

This replaced a Dockerfile that ran `go mod tidy`, which re-resolves the module
graph against whatever network the build agent has. That is the single most
common way this image broke.

## Match explanation

The feature that makes blocked traffic actionable rather than merely visible.

For Tetragon, `k8s.ExplainTracingMatch` walks the policy looking for hooks whose
`call` matches the event's function, then evaluates each selector as the
conjunction it is. A selector with a clause the event fails is not reported —
it did not fire, so it is not the cause. Clauses that did match are returned
with their JSON path (`kprobes[0].selectors[1].matchBinaries[0]`), the operator,
the configured values, and the event value that satisfied them. Positive
clauses also carry the exclusion dimension that would stop the match; negative
ones do not, because narrowing a `NotIn` does not help.

For Cilium, `ExplainNetworkMatch` uses the policy attribution Cilium 1.15+ puts
on the flow (`IngressDeniedBy` and friends) and maps it back onto the specific
ingress or egress rule. When no rule covers the peer, it says so explicitly:
that is default-deny doing its job once a policy selects the endpoint, which is
a different situation from a rule that matched.

## Self-protection

Two layers, both server-side.

`CheckManifest` walks the submitted manifest for values that *positively*
select a namespace — `metadata.namespace`, a Tetragon `podSelector` expression,
a Cilium `endpointSelector` on the namespace label — and rejects protected
ones. Values under `NotIn`, `NotEquals` and friends are exclusions, not
selections, and are skipped: rejecting the safest possible policy would be an
unhelpful kind of safe.

`Exempt` then injects `app.kubernetes.io/part-of NotIn [isovalent-control]`
into every TracingPolicy's `podSelector`. It is idempotent, and it leaves
Cilium policies alone because they select by `endpointSelector` — a
`podSelector` there would be silently meaningless.

## Frontend

Next.js app router, one client component per page, Tailwind with a small token
layer in `globals.css`. Two shared pieces of behaviour are worth knowing about:

`lib/api.ts` wraps every request, logs failures, and distinguishes a network
error from an HTTP error — the former gets a message naming the API base URL
and pointing at Diagnostics, because "something went wrong" is not a diagnosis.
It also batches browser log lines to `/api/v1/logs/client`, so frontend and
backend problems land in one timeline instead of half the story sitting in a
devtools console nobody has open.

`AppShell` loads `/api/v1/config` once and provides it to the tree. The
navigation model in `components/nav.ts` is data, shared by the sidebar and the
command palette, and entries whose backing integration is not configured are
hidden rather than greyed out. A menu entry that can only ever show an error
teaches people to ignore the menu.

## Alert dispatch

Alerts are deduplicated per kind and title within a 60-second window before any
sink sees them, so one noisy policy cannot page you four hundred times.

Every sink shares one delivery function, including the "Send test" button. A
test that exercises different code from the real path is worse than no test.
Syslog is the exception to the HTTP shape — it dials UDP or TCP directly, with
RFC6587 octet framing on TCP — and renders either RFC5424 or CEF.

## Testing

The suite covers the logic where being wrong is expensive and silent:

- `guard` — that a protected namespace is rejected through all three places a
  manifest can name one, and that a `NotIn` exclusion is *not* rejected.
- `k8s/exclusions` — that exclusions merge rather than duplicate, and that a
  path exclusion refuses an already-constrained argument index.
- `k8s/explain` — that a clause is only reported when the event actually
  satisfied it, and that an unrelated hook is not blamed.
- `hits` — that only the five expressible dimensions are offered, and that
  generated pod labels are filtered out.
- `investigate` — that filters span both sources and that policy attribution
  survives normalisation.
- `alerts` — the syslog priority arithmetic and CEF severity mapping.
