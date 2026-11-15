# Domain

## Ubiquitous Language

| Code name | Business concept |
|---|---|
| `ServiceOrder` | Aggregate: customer and vehicle snapshots, works, supplies, status, `total_amount`. Id is a ULID |
| `WorkItem` | Billable service copied from the Catalog when added: name, description, unit price |
| `SupplyItem` | Part reserved in the Catalog: name, description, unit price, version, quantity, reservation id |
| `Saga` (`saga_instance`) | The approval flow of one order: step, status, deadline, which commands were sent |
| `CustomerSnapshot`, `VehicleSnapshot` | Data copied from Customer and Vehicle at creation; never re-read |

## Status Lifecycle

```
NEW → RECEIVED → IN_DIAGNOSIS → AWAITING_APPROVAL → IN_PROGRESS → COMPLETED → DELIVERED
                                                         ↓              ↓
                                                      REJECTED      CANCELLED   (terminal)
```

- Works and supplies change only in `NEW` (works) and `IN_DIAGNOSIS` (supplies). Otherwise
  `ErrServiceOrderNotNew` / `ErrServiceOrderNotInDiagnosis` (409).
- `AWAITING_APPROVAL` exists only while the customer has not decided. `accept` moves the order to
  `IN_PROGRESS` at once; payment and execution do not change the status.
- `IN_PROGRESS → REJECTED` and `IN_PROGRESS → CANCELLED` are allowed only from the saga.
- `REJECTED` is never cancelable. A status change is always accompanied by a
  `ServiceOrderStatusChanged` event and a status-transition log.

## Saga Rules (summary)
- `send` starts the saga at `BUDGET`; the order stays `IN_DIAGNOSIS` until `BudgetGenerated`.
- Replies are processed only when the saga is `RUNNING` and at the expected `step`; otherwise ignored.
- Deadlines: `SAGA_STEP_TIMEOUT` for each step, `SAGA_PAYMENT_TIMEOUT` for payment. A timed-out
  `BUDGET` step fails the saga and keeps the order in `IN_DIAGNOSIS`.
- Compensation order: `CancelExecution` → `RefundPayment` → `CancelBudget` → `ReleaseStock`, only for
  what the saga flags show was sent. Compensations are idempotent, never "delete".

## Pricing
```
total = Σ work.unit_price + Σ (supply.unit_price × quantity)
```
Recalculated in the same transaction on every addition or removal of a work or supply.

## Supply Reservation
Supplies are reserved in the Catalog (`POST /internal/reservations`) when added, and released
(`DELETE /internal/reservations/{id}`) when removed or when the order is cancelled or rejected
(`ReleaseStock`). A failed reservation returns the Catalog error; reservations already made are
released when the order cannot be stored. A Catalog `404` on release means "already released".

## Roles
Constants and groups live in `internal/pkg/auth/roles.go`: `ADMIN`, `ATTENDANT`, `MECHANIC`, `CUSTOMER`.
`ADMIN` is accepted on every protected route. The customer role is checked against the `user_id`
in the JWT claims for `accept` and `reject`; a different user gets `403`.

## Input Validation
- Identifiers must be non-empty path values (`400` otherwise).
- Supply `amount` must be positive; `services` and `supplies` lists must not be empty where required.
- Money is `NUMERIC(10,2)` in the database and `decimal.Decimal` in Go. Never `float64`.
