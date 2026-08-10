.PHONY: backend-run backend-build frontend-dev frontend-build lint test docker helm-lint

backend-run:
	cd backend && go run ./cmd/server

backend-build:
	cd backend && CGO_ENABLED=0 go build -o ../bin/isovalent-control ./cmd/server

frontend-dev:
	cd frontend && npm run dev

frontend-build:
	cd frontend && npm run build

lint:
	cd backend && go vet ./...
	cd frontend && npm run lint --if-present

test:
	cd backend && go test ./...

docker:
	docker build -t isovalent-control-backend:dev backend/
	docker build -t isovalent-control-frontend:dev frontend/

helm-lint:
	helm lint charts/isovalent-control
