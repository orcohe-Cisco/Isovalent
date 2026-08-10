FROM golang:1.24 AS build
# GOTOOLCHAIN=local pins the build to Go 1.24 (no surprise toolchain downloads);
# `go mod tidy` regenerates a complete go.sum from the pinned module graph, so
# the build works in any normal network (e.g. an `az acr build` agent) whether
# or not go.sum is present.
ENV GOTOOLCHAIN=local
WORKDIR /src
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=mod -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8081
# Numeric UID (distroless "nonroot" = 65532) so Kubernetes runAsNonRoot can
# verify the user without a named-user lookup.
USER 65532:65532
ENTRYPOINT ["/server"]
