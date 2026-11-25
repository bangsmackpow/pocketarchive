.PHONY: help dev-up dev-down engine-test engine-build web-install web-dev web-build test compose-check

help:
	@echo "PocketArchive development targets"
	@echo "  make dev-up        Start Postgres, Meilisearch and Valkey"
	@echo "  make dev-down      Stop local dependencies"
	@echo "  make engine-test   Run Go tests"
	@echo "  make engine-build  Build the engine binary"
	@echo "  make web-install   Install frontend dependencies"
	@echo "  make web-dev       Run the TanStack Start dev server"
	@echo "  make web-build     Production frontend build"
	@echo "  make compose-check Validate both Compose files"

dev-up:
	docker compose -f deploy/dev/docker-compose.dev.yml up -d

dev-down:
	docker compose -f deploy/dev/docker-compose.dev.yml down

engine-build:
	cd engine && go build ./...

engine-test:
	cd engine && go test ./... && go vet ./...

web-install:
	cd web && npm ci

web-dev:
	cd web && npm run dev

web-build:
	cd web && npm run build

test: engine-test web-build

compose-check:
	docker compose -f deploy/portainer/docker-compose.yml --env-file deploy/portainer/.env.example config >/dev/null
	docker compose -f deploy/dev/docker-compose.dev.yml config >/dev/null
