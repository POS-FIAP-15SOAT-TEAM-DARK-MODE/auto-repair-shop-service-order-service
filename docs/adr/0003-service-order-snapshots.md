# ADR 0003 — Snapshots of customer, vehicle, works and supplies in the service order

- Status: Accepted
- Date: 2026-11-01
- Tags: data-ownership, consistency, service-boundaries

## Context

In the monolith the service order read the customer, the vehicle, the works and the supplies from the
same database, with joins and foreign keys. After the split each of those belongs to another service
(Customer, Catalog), and the order must still show them, price them and keep a record of what was
agreed with the customer even after the source data changes.

## Decision

The order stores a snapshot of the data it needs at creation or when the item is added, and does not
query the other services to read it again:

- Customer: `ID, UserID, Name, Email, Type, Document, CompanyName, Phone`. `UserID` is the identity
  that the JWT carries, so the `accept` and `reject` guards need no extra call.
- Vehicle: `ID, LicensePlate, Brand, Model, Year`.
- Works: `name`, `description`, `unit_price`, taken from the Catalog when the work is added.
- Supplies: `name`, `description`, `unit_price`, `version`, `quantity` and `reservation_id`. The
  `reservation_id` is the Catalog reservation that `ReleaseStock` and the supply removal use.

The total is computed on the order and recalculated in the same transaction on every insertion or
removal of a work or supply: `total_amount = sum(work.unit_price) + sum(supply.unit_price * quantity)`.
The saga sends the works and supplies with these snapshot values, so the budget shown to the customer
is the one that was priced.

Reads (`GET`) use only the local database, so the order keeps working when Customer or Catalog is
down.

## Consequences

- Reads are fast and do not depend on the availability of other services.
- The agreed price does not change when a supply price changes later. A new price applies only to
  orders created or updated afterwards.
- The copies can become stale if the source changes. This is intended for the order history, but
  an attendant must add a work again to pick up a new price.
- Customer or vehicle data corrected after creation is not reflected in existing orders. Customer
  and vehicle deletion is blocked while an order exists, through `GET /internal/service-orders/exists`.
- The schema holds the snapshot columns; see `migrations/000001_init_schema.up.sql`.
