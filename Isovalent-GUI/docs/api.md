# API

Everything the console does, it does through this API. The UI has no
privileged back channel, so anything you can click, you can automate.

The authoritative contract is the OpenAPI document, served by the backend and
rendered in the console under **API → Reference**:

```
GET /api/openapi.yaml
```

This page covers the parts that are policy rather than schema.

## Authentication

Both schemes use `Authorization: Bearer <token>`.

**OIDC JWT** for humans. Roles come from the `groups` claim:

```
ic:viewer                 read-only, all namespaces
ic:editor:shop,web        policy edit rights in those namespaces (globs work)
ic:admin                  full control, including cluster-scoped policies
```

**API tokens** for machines. Create one under **API → Tokens**, or set
`IC_API_TOKENS=name:role:token`. Only a SHA-256 hash is stored; the plaintext
is returned once at creation and there is no endpoint that can read it back.
That is inconvenient on purpose.

WebSocket clients cannot set headers, so they pass `?access_token=`.

```bash
export IC=http://localhost:8081
export IC_TOKEN=ic_...

curl -sS -H "Authorization: Bearer $IC_TOKEN" "$IC/api/v1/overview" | jq
```

## Conventions

- The reserved namespace path segment `-` means cluster scope.
- Time windows take either `window=5m|15m|1h|6h|1d|7d|30d` or explicit RFC3339
  `since` and `until`.
- List filters can be repeated or comma-separated. Values within one parameter
  are ORed; different parameters are ANDed.
- Errors are `{"error": "..."}` with a meaningful status. `403` from a policy
  write usually means self-protection, not RBAC — the message says which.

## Recipes

### Find what is firing, and quieten it

```bash
# Which policies are noisy?
curl -sS -H "Authorization: Bearer $IC_TOKEN" "$IC/api/v1/hits" \
  | jq -r '.policies[] | select(.total > 0) | "\(.total)\t\(.policy)\t\(.topBinary)"' \
  | sort -rn | head

# What fired one of them?
curl -sS -H "Authorization: Bearer $IC_TOKEN" \
  "$IC/api/v1/hits/-/watch-sensitive-files" \
  | jq '.detail.values.binaries[:5]'

# Preview an exclusion without touching the cluster
curl -sS -H "Authorization: Bearer $IC_TOKEN" -H 'Content-Type: application/json' \
  -X POST "$IC/api/v1/hits/-/watch-sensitive-files/exclusions?dryRun=true" \
  -d '{"binaries":["/usr/bin/fluent-bit"]}' | jq '{notes, diff}'

# Apply it
curl -sS -H "Authorization: Bearer $IC_TOKEN" -H 'Content-Type: application/json' \
  -X POST "$IC/api/v1/hits/-/watch-sensitive-files/exclusions" \
  -d '{"binaries":["/usr/bin/fluent-bit"]}'
```

`notes` is the part worth reading. It reports which selectors the exclusion
actually reached and which it could not — a path exclusion that failed to
apply to three of your hooks is exactly the sort of thing that should not be
silent.

### Search history across both data sources

```bash
# Everything blocked in the last hour in one namespace
curl -sS -G -H "Authorization: Bearer $IC_TOKEN" "$IC/api/v1/investigate" \
  --data-urlencode 'window=1h' \
  --data-urlencode 'namespace=shop' \
  --data-urlencode 'blocked=true' | jq '.rows[] | {time, source, summary, policy}'

# Every POST to an admin path, whatever the verdict
curl -sS -G -H "Authorization: Bearer $IC_TOKEN" "$IC/api/v1/investigate" \
  --data-urlencode 'window=1d' \
  --data-urlencode 'method=POST' \
  --data-urlencode 'path=/admin' | jq '.total'
```

The response includes `facets`: every filterable value present in the result
set, with counts. That is what the UI's clickable chips are built from, and it
is the cheapest way to discover what a filter would return before applying it.

### Ask why something was blocked

```bash
ROW=$(curl -sS -G -H "Authorization: Bearer $IC_TOKEN" "$IC/api/v1/investigate" \
  --data-urlencode 'blocked=true' --data-urlencode 'limit=1' | jq '.rows[0]')

curl -sS -G -H "Authorization: Bearer $IC_TOKEN" "$IC/api/v1/investigate/explain" \
  --data-urlencode "source=$(echo "$ROW" | jq -r .source)" \
  --data-urlencode "policy=$(echo "$ROW" | jq -r .policy)" \
  --data-urlencode "raw=$(echo "$ROW" | jq -c .raw)" | jq '{summary, clauses}'
```

`clauses` gives the JSON path of each matching condition inside the policy —
`kprobes[0].selectors[1].matchBinaries[0]` — the operator, the configured
values, and which one the event satisfied. Clauses that could be narrowed carry
an `excludable` dimension naming the exclusion that would stop the match.

### Dry-run before enforcing

```bash
curl -sS -H "Authorization: Bearer $IC_TOKEN" -H 'Content-Type: application/json' \
  -X POST "$IC/api/v1/tracingpolicies/dryrun?window=1d" \
  --data-binary @policy.json | jq '{matched, wouldEnforce, advice, byBinary}'
```

This replays stored events through the proposed policy. It is the only honest
way to preview a `Sigkill` policy, because the alternative is finding out in
production and there is no undo.

### CI: apply a policy from a pipeline

```bash
curl -fsS -H "Authorization: Bearer $IC_TOKEN" -H 'Content-Type: application/json' \
  -X PUT "$IC/api/v1/policies/CiliumNetworkPolicy/shop/checkout-egress" \
  --data-binary @checkout-egress.json
```

Add `?mode=pr` and the change becomes a pull request instead, if
`IC_GITHUB_REPO` and `IC_GITHUB_TOKEN` are configured.

A `403` here with a message about a protected namespace is self-protection
refusing to let a pipeline write a policy that could take the console down.
There is no override flag; change the manifest.

### Streams

```bash
websocat "ws://localhost:8081/ws/events?access_token=$IC_TOKEN"
```

Topics: `flows`, `events`, `alerts`, `logs`. Slow consumers get messages
dropped rather than being allowed to backpressure the agents.

## Auditing

Every mutating call is recorded with the caller, the source IP, the before and
after manifests and a field-level diff — including the calls that were denied.
An audit log that only records successes tells you nothing about the
interesting afternoon.

```bash
curl -sS -G -H "Authorization: Bearer $IC_TOKEN" "$IC/api/v1/audit" \
  --data-urlencode 'window=7d' --data-urlencode 'q=exclude' \
  | jq '.entries[] | {time, actor, action, target, outcome}'
```

## Health and diagnostics

`/healthz` and `/metrics` are unauthenticated so probes and scrapers work
without credential plumbing. `/healthz` reports the version, commit and build
date of the running binary, which is the fastest way to tell whether the
cluster is running the image you think it is.

`/api/v1/diagnostics` probes every dependency in parallel and returns, for each
one, a status, a latency, what was probed and — when it fails — a concrete next
step. Use it in a smoke test:

```bash
curl -fsS -H "Authorization: Bearer $IC_TOKEN" "$IC/api/v1/diagnostics" \
  | jq -e '.status == "ok"'
```
