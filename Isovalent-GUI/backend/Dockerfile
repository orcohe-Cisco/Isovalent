# Hermetic backend build.
#
# Dependencies are vendored (backend/vendor), so this image builds with no
# module downloads at all: no proxy.golang.org, no GOPROXY, no go.sum
# reconciliation. That is deliberate — `go mod tidy` inside a Docker build is
# the single most common way this image breaks, because it re-resolves the
# module graph against whatever network the build agent happens to have.
FROM golang:1.24 AS build
ENV GOTOOLCHAIN=local GOFLAGS=-mod=vendor GOPROXY=off CGO_ENABLED=0
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
WORKDIR /src
COPY . .
# The version is stamped in so /healthz can prove which build is running —
# a stale image behind a reused tag is the most common "it's broken" report.
RUN GOOS=linux go build -trimpath \
      -ldflags="-s -w \
        -X github.com/isovalent-control/isovalent-control/backend/internal/version.Version=${VERSION} \
        -X github.com/isovalent-control/isovalent-control/backend/internal/version.Commit=${COMMIT} \
        -X github.com/isovalent-control/isovalent-control/backend/internal/version.Date=${BUILD_DATE}" \
      -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8081
# Numeric UID (distroless "nonroot" = 65532) so Kubernetes runAsNonRoot can
# verify the user without a named-user lookup.
USER 65532:65532
ENTRYPOINT ["/server"]
