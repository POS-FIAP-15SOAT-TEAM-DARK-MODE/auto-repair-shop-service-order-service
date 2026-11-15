# Architecture

## Layer Map

```
HTTP → routes.go / controller.go (internal/service_order)
           → service.go (use cases, status rules)  ──→ saga orchestrator (internal/saga)
           → repository/ (Postgres and in-memory)  ──→ PostgreSQL (service_order)
           → clients/ (Customer and Catalog REST)
Messaging: internal/pkg/messaging relay (outbox → SNS) and consumer (SQS → orchestrator)
Domain (internal/service_order/domain): framework-free types, errors, interfaces.
```

## Layer Contracts
- **Controller / routes**: parse and validate HTTP input, call the service, map the result. Zero business logic.
- **Service and saga**: all business rules, status transitions, saga steps, price recalculation.
- **Repository**: all SQL. Nothing else in the service writes SQL.
- **Domain**: no imports of gin, pq, net/http or any `internal/` adapter package.

## Composition Root
`internal/app/bootstrap/bootstrap.go` is the only wiring file. It creates the DB pool, UoW,
repositories, REST clients, orchestrator, service, controller and the workers (HTTP server,
saga-deadline loop, outbox relay, SQS consumer). Constructor injection only.

## Unit of Work and Transactions
- `uow.NewTransactionalUoW(db)` runs the steps of `Execute` in one transaction; steps read the
  transaction from the context.
- Repositories hold no `*sql.DB` used for writes; they call `uow.Conn(ctx, db)`, which returns the
  transaction attached to the context or the pool when there is none.
- The consumer opens a transaction per message and attaches it with `uow.WithTx`; handlers receive
  `(ctx, tx, env)` so the state change, `saga_instance`, `outbox` and `processed_message` commit together.
- `uow.OnCommit` schedules side effects (the status notifier) to run after the commit.

## Saga Orchestrator
- State lives in `saga_instance`; one `RUNNING` saga per order (partial unique index).
- Every transition is one transaction. Replies that do not match the saga state are logged and dropped.
- Compensations are fire-and-forget; see `rules/domain.md` and `docs/adr/0002-orchestrated-saga.md`.

## Dynamic SQL
Use `internal/pkg/db` (`QueryBuilder`) for filters and pagination. Never concatenate user input into SQL.
