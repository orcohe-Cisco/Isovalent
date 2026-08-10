# Contributing to Isovalent-Control

Thanks for your interest! This project aims to be the community GUI for the
Cilium / Hubble / Tetragon stack.

## Development setup

Prerequisites: Go ≥ 1.24, Node ≥ 20, (optionally) a kind cluster with Cilium.

```bash
make backend-run    # starts the Go API on :8081 in mock mode
make frontend-dev   # starts Next.js on :3000
```

## Pull requests

1. Fork and create a feature branch (`feat/…`, `fix/…`, `docs/…`).
2. `make lint test` must pass (`go vet`, `go test ./...`, `next build`).
3. Keep commits atomic; sign off with `git commit -s` (DCO).
4. Open a PR against `main` — the CI workflow runs the same checks.

## Where to help

- **Backend (Go):** Timescape/historical flows, alert routing sinks, GitOps PR
  apply mode, ClickHouse event store.
- **Frontend (Next.js):** dry-run policy simulation UI, flow replay
  ("time-travel"), dark-mode polish, i18n.
- **Charts/Deploy:** production Helm hardening, NetworkPolicy self-policies.

## Code style

- Go: `gofmt`, `go vet`, table-driven tests, no panics in request paths.
- TypeScript: strict mode, functional components, Tailwind utility classes.

## Conduct

We follow the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md).
