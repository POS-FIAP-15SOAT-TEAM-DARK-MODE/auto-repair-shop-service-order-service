# Playbook: Add a Domain Aggregate

Use this when a new aggregate or sub-resource is added to service-order-service.
Most changes touch the existing aggregate; check `rules/domain.md` before adding a new one.

## 1. Domain — `internal/service_order/domain/`
- Struct with the id type (ULID for orders, UUID for other entities), `CreatedAt`/`UpdatedAt` if persisted.
- Sentinel errors as `var ErrXxx = errors.New(...)`; add a status to `http_errors.go` when it has one.
- Repository interface next to the service that uses it. Add `//go:generate` for mockery when a mock is needed.
- No imports from gin, lib/pq, net/http or adapter packages.

## 2. Repositories — `internal/service_order/repository/`
- `*_postgres.go`: raw SQL through `uow.Conn(ctx, db)`; queries in `*_queries.go`.
- `*_in_memory.go`: same interface, used by unit tests only.

## 3. Service — `internal/service_order/service.go`
- All business rules here. Mutations inside `uow.Execute(ctx, steps...)`.
- Emit `ServiceOrderStatusChanged` through the outbox when the status changes; notify after commit.

## 4. Controller and routes
- Parse and validate in `controller.go`; map results in `mappers.go`; DTOs in `adapters/`.
- Register the route in `routes.go` with the role group from `internal/pkg/auth/roles.go`.
- Update `docs/swagger.yaml` and the endpoint table in `rules/api.md` and `README.md`.

## 5. Migration
Follow `add-migration.md`. Add foreign keys to the parent table and indexes for filters.

## 6. Tests
- Unit: service with the in-memory repository and `uow.NewInMemoryUoW()`.
- Integration (`-tags=integration`): Postgres repository, rolled back after each test.
- Coverage: 80% total; 90% when the code is in `internal/saga` or `internal/service_order/domain`.

## 7. Mocks
`make mockgen`. Never edit generated files.
