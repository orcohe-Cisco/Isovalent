# Contributing

## Development setup

Prerequisites: Go ≥ 1.24, Node ≥ 20, and a cluster with Cilium and Tetragon.

There is no offline mode. The console reads a live cluster or it reads nothing,
so development needs one. `kind` is fine:

```bash
kind create cluster --config deploy/local/kind.yaml
./run.sh
```

To iterate on the code rather than the deployment, run both halves locally
against that cluster:

```bash
kubectl proxy &                                                 # :8001
kubectl -n kube-system port-forward svc/hubble-relay 4245:80 &
kubectl -n kube-system port-forward svc/tetragon 54321:54321 &
kubectl -n kube-system port-forward svc/hubble-ui 12000:80 &

make dev-backend    # :8081
make dev-frontend   # :3000
```

## Before opening a pull request

```bash
make check          # go vet, go test, bash 3.2 check, OpenAPI parse
make build          # go build + next build
```

CI runs the same things, plus a check that `backend/vendor` matches `go.mod`.

1. Branch as `feat/…`, `fix/…` or `docs/…`.
2. Keep commits atomic. Sign off with `git commit -s` (DCO).
3. Open the PR against `main`.

## Dependencies

The backend vendors its dependencies. After changing `go.mod`:

```bash
make vendor         # go mod tidy && go mod vendor
```

Commit `vendor/` along with `go.mod` and `go.sum`. CI fails if they drift.

This matters more than it looks: the container build runs with `-mod=vendor`
and `GOPROXY=off`, so it needs no network at all. Reintroducing a module
download into the Dockerfile — `go mod tidy` especially — will work on your
machine and break on a build agent with different egress.

## Shell scripts must run on bash 3.2

macOS still ships bash 3.2 from 2007. No `mapfile`, no `readarray`, no
associative arrays, no `${var,,}`, no `&>>`. `make check-scripts` enforces it,
and it is wired into CI.

Arrays under `set -u` are a subtler version of the same trap: `"${arr[@]}"` on
an empty array is an unbound-variable error on 3.2 and fine on 4.4. Prefer
space-separated strings.

## Things worth knowing before you change them

**`internal/guard`** is what stops a policy authored in the console from taking
the console down. If you change the label it exempts, change the deployment
manifests and the Helm chart's `_helpers.tpl` in the same commit. A rename that
lands in one place and not the other silently disables the protection.

**`internal/k8s/exclusions.go`** only offers the five exclusion dimensions
Tetragon can actually express. If you add a sixth, make sure it does something
— an exclusion that silently matches nothing is worse than no exclusion,
because people stop looking.

**`internal/k8stest`** is a test double, not a runtime mode. It must stay
importable only from `_test` files, so the server binary never links it. If you
find yourself wanting it in `main`, that is the demo-data path coming back.

**Everything that mutates goes through `s.record`** — including the denied and
error paths. An audit log that only records successes tells you nothing about
the interesting afternoon.

## Code style

- Go: `gofmt`, `go vet`, table-driven tests, no panics in request paths.
- TypeScript: strict mode, functional components, Tailwind utilities, design
  tokens from `globals.css` rather than hard-coded colours.
- Comments explain *why*. The code already says what.

## Where help is wanted

- **Backend:** ClickHouse as an alternative history store; Timescape support;
  richer Cilium policy simulation; more exclusion mechanisms as Tetragon gains
  them.
- **Frontend:** saved investigations, diff view between two policy revisions,
  keyboard navigation in the results table.
- **Deploy:** hardening the Helm chart for production, an authenticating proxy
  in front of the embedded Grafana rather than the anonymous-viewer default.

## Conduct

We follow the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md).
