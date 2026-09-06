# isovalent-control — common tasks.
#   make help    to see everything
VERSION := $(shell cat VERSION)
NAMESPACE ?= isovalent-control
GOFLAGS := -mod=vendor

.PHONY: help
help:  ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ---------------------------------------------------------------- deploy ---
.PHONY: run
run:  ## Install, deploy and port-forward everything (any Kubernetes)
	./run.sh

.PHONY: forward
forward:  ## Re-establish the port-forwards only
	./run.sh --forward-only

.PHONY: uninstall
uninstall:  ## Remove isovalent-control from the cluster
	./run.sh --uninstall

.PHONY: compose
compose:  ## Run locally with Docker, against a port-forwarded cluster
	docker compose -f deploy/docker-compose.yaml up --build

## ------------------------------------------------------------------- dev ---
.PHONY: dev-backend
dev-backend:  ## Run the Go backend on :8081 (needs kubectl proxy + port-forwards)
	cd backend && GOFLAGS=$(GOFLAGS) go run -ldflags "-X github.com/isovalent-control/isovalent-control/backend/internal/version.Version=$(VERSION)-dev" ./cmd/server

.PHONY: dev-frontend
dev-frontend:  ## Run the Next.js dev server on :3000
	cd frontend && npm install && npm run dev

.PHONY: build
build:  ## Build both, as CI does
	cd backend && GOFLAGS=$(GOFLAGS) CGO_ENABLED=0 go build -ldflags "-X github.com/isovalent-control/isovalent-control/backend/internal/version.Version=$(VERSION)" -o ../bin/isovalent-control ./cmd/server
	cd frontend && npm ci && npm run build

.PHONY: test
test:  ## Go vet + tests
	cd backend && GOFLAGS=$(GOFLAGS) go vet ./... && GOFLAGS=$(GOFLAGS) go test ./...

.PHONY: vendor
vendor:  ## Refresh backend/vendor after a dependency change
	cd backend && go mod tidy && go mod vendor

.PHONY: check-scripts
check-scripts:  ## Verify the shell scripts still run on macOS bash 3.2
	bash hack/check-bash32.sh

.PHONY: check-openapi
check-openapi:  ## Parse the OpenAPI document
	python3 -c "import yaml,sys; yaml.safe_load(open('backend/internal/server/openapi.yaml')); print('openapi.yaml parses')"

.PHONY: check-manifest
check-manifest:  ## Render and parse the deployment manifest
	@IC_IMAGE_PREFIX=ghcr.io/example/ IMAGE_TAG=$(VERSION) NAMESPACE=$(NAMESPACE) \
	 CLUSTER_NAME=test HUBBLE_RELAY=h:80 TETRAGON_ADDR=t:54321 \
	 HUBBLE_UI_URL=http://u:80 GRAFANA_URL=http://g:80 \
	 envsubst < deploy/kubernetes/isovalent-control.yaml \
	 | python3 -c "import yaml,sys; d=[x for x in yaml.safe_load_all(sys.stdin) if x]; print(len(d),'documents OK')"

.PHONY: check
check: test check-scripts check-openapi  ## Everything CI runs that does not need a cluster

.PHONY: docker
docker:  ## Build both images locally
	docker build --build-arg VERSION=$(VERSION) -t isovalent-control-backend:$(VERSION) backend/
	docker build -t isovalent-control-frontend:$(VERSION) frontend/

.PHONY: helm-lint
helm-lint:  ## Lint the Helm chart
	helm lint charts/isovalent-control

## --------------------------------------------------------------- library ---
.PHONY: library
library:  ## Regenerate the Tetragon policy library from a cilium/tetragon checkout
	@test -n "$(TETRAGON)" || (echo "usage: make library TETRAGON=/path/to/cilium/tetragon"; exit 1)
	python3 gen/gen_library.py $(TETRAGON) gen/authored frontend/src/lib/policyLibrary.ts

.PHONY: version
version:  ## Print the version this tree builds
	@echo $(VERSION)
