#!/usr/bin/env bash
# Adds GitHub-mirror replace directives to backend/go.mod for environments
# where proxy.golang.org and the Go vanity-import hosts (golang.org,
# google.golang.org, gopkg.in) are unreachable but github.com is allowed.
# Usage:  hack/offline-replaces.sh apply | drop
set -euo pipefail
cd "$(dirname "$0")/../backend"

REPLACES=(
  "google.golang.org/grpc=github.com/grpc/grpc-go@v1.67.3"
  "google.golang.org/protobuf=github.com/protocolbuffers/protobuf-go@v1.36.5"
  "golang.org/x/net=github.com/golang/net@v0.33.0"
  "golang.org/x/sys=github.com/golang/sys@v0.28.0"
  "golang.org/x/text=github.com/golang/text@v0.21.0"
  "google.golang.org/genproto/googleapis/rpc=github.com/googleapis/go-genproto/googleapis/rpc@v0.0.0-20240814211410-ddb44dafa142"
)

case "${1:-apply}" in
  apply)
    for r in "${REPLACES[@]}"; do go mod edit -replace "$r"; done
    echo "Applied ${#REPLACES[@]} replaces. Build with: GOPROXY=direct GOSUMDB=off GOFLAGS=-mod=mod go build ./..."
    ;;
  drop)
    for r in "${REPLACES[@]}"; do go mod edit -dropreplace "${r%%=*}"; done
    echo "Dropped replaces."
    ;;
  *) echo "usage: $0 apply|drop" >&2; exit 1 ;;
esac
