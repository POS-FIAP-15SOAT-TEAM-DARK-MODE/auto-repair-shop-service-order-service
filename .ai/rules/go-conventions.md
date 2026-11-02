# Go Conventions

## Tech Stack
Go 1.27 · Gin · PostgreSQL 15 · `lib/pq` (raw SQL, no ORM) · `golang-migrate` (CLI) · `golang-jwt/jwt/v5` ·
Zap · Testify · `shopspring/decimal` · `google/uuid` · `oklog/ulid/v2`.

## IDs
- `oklog/ulid/v2` for `service_order` ids (sortable by creation time, stored as `VARCHAR(26)`).
- `google/uuid` for every other id (`VARCHAR(36)` columns, `UUID` for `processed_message` and `outbox.message_id`).
- Ids are generated in the service or the saga, never in SQL defaults.

## Monetary Values
Use `shopspring/decimal.Decimal`. JSON money fields are strings (`"221.00"`), never numbers.
Database column type: `NUMERIC(10,2)`.

## No Framework Leakage
- Domain: no imports of `gin`, `lib/pq`, `net/http` or adapter packages.
- Service and saga: depend on domain interfaces and `internal/pkg/uow`, not on HTTP or SQL types.
- Gin types stay in `controller.go`, `routes.go` and `internal/app/routing`.

## Test Doubles
No mocks are generated. Unit tests use the in-memory implementations that satisfy the same
interfaces (`repository.NewInMemory()`, `saga.NewMemoryRepository()`, `saga.NewMemoryOutbox()`,
`uow.NewInMemoryUoW()`). Add a generator only when a test cannot use them, and document it here and in
`Makefile`.

## Error Patterns
- Domain errors are sentinels (`var ErrXxx = errors.New(...)`) in the domain package.
- HTTP status is registered next to the sentinels (`internal/service_order/http_errors.go`) with
  `web.RegisterErrorStatus`; handlers never switch on error text.
- Upstream failures become `ErrUpstreamUnavailable` (502); never expose internal error details.
- Input validation sentinels are registered with `web.RegisterValidationErrors` and answer 400 with the `errors` list.
- Response bodies are `{"code", "error"}`, or `{"code", "errors"}` for 400 (see `rules/api.md`).

## Configuration
Read through `internal/pkg/env` (`GetString`, `GetInt`, `GetTimeDuration`). Required settings are
listed in `.env.example`. Messaging is disabled, with a warning, when the queue or topic variables are empty.
