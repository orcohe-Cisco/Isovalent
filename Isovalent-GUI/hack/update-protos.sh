#!/usr/bin/env bash
# Refreshes the vendored generated protos in backend/pkg/protos from the
# pinned upstream tags. Only Go import paths inside import blocks are
# rewritten; raw descriptor bytes are left untouched.
set -euo pipefail
CILIUM_TAG="${CILIUM_TAG:-v1.17.9}"
TETRAGON_TAG="${TETRAGON_TAG:-api/v1.4.1}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/backend/pkg/protos"
MOD="github.com/isovalent-control/isovalent-control/backend"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

git clone --depth 1 --filter=blob:none --sparse --branch "$CILIUM_TAG" https://github.com/cilium/cilium "$TMP/cilium"
git -C "$TMP/cilium" sparse-checkout set api/v1
git clone --depth 1 --filter=blob:none --sparse --branch "$TETRAGON_TAG" https://github.com/cilium/tetragon "$TMP/tetragon"
git -C "$TMP/tetragon" sparse-checkout set api

cp "$TMP/cilium/api/v1/flow/flow.pb.go" "$DEST/flow/"
cp "$TMP/cilium/api/v1/observer/observer.pb.go" "$TMP/cilium/api/v1/observer/observer_grpc.pb.go" "$DEST/observer/"
cp "$TMP/cilium/api/v1/relay/relay.pb.go" "$DEST/relay/"
cp "$TMP"/tetragon/api/v1/tetragon/{bpf,capabilities,events,sensors,stack,tetragon,types}.pb.go \
   "$TMP/tetragon/api/v1/tetragon/sensors_grpc.pb.go" "$DEST/tetragon/"

# Rewrite ONLY import-block lines (tab-indented quoted paths); descriptor
# string literals contain the same substring and must not change length.
find "$DEST" -name '*.pb.go' -exec perl -i -pe \
  's{^(\t(?:\w+ )?")github\.com/cilium/cilium/api/v1(/(?:flow|relay|observer)")}{$1'"$MOD"'/pkg/protos$2}' {} +
echo "Protos refreshed: cilium@$CILIUM_TAG tetragon@$TETRAGON_TAG"
