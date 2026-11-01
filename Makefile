-include .env
export

# Stubs of Customer and Catalog start with `make up` (see local/stubs).
export COMPOSE_PROFILES ?= stubs

run:
	go run cmd/service/main.go

test:
	LOG_LEVEL=PANIC go test ./... --race -v

test-integration:
	LOG_LEVEL=PANIC go test -tags=integration ./internal/integration/... -v

coverage:
	LOG_LEVEL=PANIC go test ./... --coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

up:
	@test -f .env || cp .env.example .env
	docker compose up --build

down:
	docker compose down -v

migrate-install:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.1

migrate-up:
	migrate -path migrations -database "postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/service_order?sslmode=disable" up

migrate-down:
	migrate -path migrations -database "postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/service_order?sslmode=disable" down

mockgen:
	go generate ./...

.PHONY: run test test-integration coverage up down migrate-install migrate-up migrate-down mockgen
