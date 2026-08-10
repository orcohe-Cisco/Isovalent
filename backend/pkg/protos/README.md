# Vendored API protos

This package vendors the **generated Go bindings** for the upstream gRPC APIs
we consume, so that `isovalent-control` builds with a minimal module graph and
without pinning the entire `github.com/cilium/cilium` module.

| Package | Upstream source | Version | License |
|---|---|---|---|
| `flow`, `observer`, `relay` | `github.com/cilium/cilium/api/v1` | `v1.17.9` | Apache-2.0 |
| `tetragon` | `github.com/cilium/tetragon/api/v1/tetragon` | `api/v1.4.1` | Apache-2.0 |

Only import paths were rewritten (`github.com/cilium/cilium/api/v1/…` →
`…/backend/pkg/protos/…`); the generated code is otherwise unmodified.
Copyright the Cilium/Tetragon authors, licensed under Apache-2.0 (see the
repository root `LICENSE`).

To refresh: run `hack/update-protos.sh` (sparse-clones the upstream repos at
the pinned tags and re-applies the import rewrite).
