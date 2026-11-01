# Go Conventions

## Tech Stack
Go 1.26 · Gin · PostgreSQL 15 · `lib/pq` (raw SQL, no ORM) · `golang-migrate` · `golang-jwt/jwt/v5` ·
Zap · Testify · mockery v2 · `shopspring/decimal` · `google/uuid` · `oklog/ulid/v2`.

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

## Mocks
`internal/pkg/uow/uow.go` carries the `//go:generate` directive for the `Executor` mock
(`internal/pkg/uow/mocks/`). Other interfaces get a directive next to their declaration.
Run `make mockgen`. Never edit generated files by hand.

## Error Patterns
- Domain errors are sentinels (`var ErrXxx = errors.New(...)`) in the domain package.
- HTTP status is registered next to the sentinels (`internal/service_order/http_errors.go`) with
  `web.RegisterErrorStatus`; handlers never switch on error text.
- Upstream failures become `ErrUpstreamUnavailable` (502); never expose internal error details.

## Configuration
Read through `internal/pkg/env` (`GetString`, `GetInt`, `GetTimeDuration`). Required settings are
listed in `.env.example`. Messaging is disabled, with a warning, when the queue or topic variables are empty.
