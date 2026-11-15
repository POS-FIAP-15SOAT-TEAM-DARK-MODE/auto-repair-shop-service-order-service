# Database

## PostgreSQL 15 Conventions
- Database `service_order`, application user `service_order_app` (least privilege, see the infra repo).
- Money: `NUMERIC(10,2)`. Never `FLOAT` or `DOUBLE PRECISION`.
- Timestamps: `TIMESTAMP WITH TIME ZONE`, UTC.
- `service_order.status` is `VARCHAR(50)`; the valid values are enforced by the domain. Add a `CHECK` when a value set changes.
- `saga_instance.step` and `status` have `CHECK` constraints; keep them in sync with `internal/saga/status.go`.

## Tables (migration `000001_init_schema`)
| Table | Purpose |
|---|---|
| `service_order` | aggregate with snapshot columns (customer, vehicle), status, `total_amount`, timestamps |
| `service_order_work` | works of an order; `UNIQUE (service_order_id, work_id)` |
| `service_order_supply` | supplies with `reservation_id`, `version`, `quantity > 0`; `UNIQUE (service_order_id, supply_id)` |
| `saga_instance` | saga state; unique partial index allows one `RUNNING` saga per order |
| `processed_message` | consumer dedupe by `message_id` |
| `outbox` | messages to publish; `published_at` is set by the relay after a successful publish |

## Migration Strategy
Versioned files only:
```
migrations/NNNNNN_description.up.sql
migrations/NNNNNN_description.down.sql
```
Never edit a migration that has been applied to any environment. Never drop or alter existing columns;
add new ones and leave the old ones in place. The `down` file must fully reverse the `up`.

Commands: `make migrate-up`, `make migrate-down` (see `Makefile`); `migrate ... version` shows the current version.
In CI and in compose the migrations run with the `migrate` CLI (`migrate/migrate` image in compose).

## Connection and Context
Repositories get the connection through `uow.Conn(ctx, db)` (transaction when one is in the context).
Read-only paths use the pool; writes always run inside `uow.Execute` or the consumer's transaction.

## Indexes and Foreign Keys
- Indexes: `service_order(status, customer_id, vehicle_id)`, the children `service_order_id` columns, `saga_instance` deadline (partial).
- Foreign keys: children of `service_order` with `ON DELETE CASCADE`. There are no foreign keys to other services' tables; cross-service references are ids only.
