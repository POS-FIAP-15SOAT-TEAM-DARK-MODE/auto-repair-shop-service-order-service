# ADR 0002 — Orchestrated saga for the service order approval

- Status: Accepted
- Date: 2026-11-01
- Tags: saga, messaging, consistency

## Context

Approving a service order spans three services. The Service Order (SO) owns the order status,
Billing issues the budget and charges the payment, and Execution queues the mechanic's work. Catalog
holds the stock that the order reserves. A single database transaction cannot cover these steps, so
the flow must be a sequence of local transactions with compensations when a step fails.

Two styles were considered:

- **Choreography**: each service reacts to the events of the previous one, and the flow lives in the
  event subscriptions. Simple to start, but the full flow is spread over four services and there is
  no single place that says what state the approval is in or which compensations apply.
- **Orchestration**: the SO holds the saga state (`saga_instance`) and decides the next command. The
  flow and its compensations are in one place, and the deadlines of each step are enforced by the
  same service.

## Decision

The Service Order orchestrates the approval saga `service-order-approval`:

- The saga is started by `PUT /v1/service-order/{id}/send`. Its state lives in `saga_instance`
  (`saga_id`, `service_order_id`, `step`, `status`, `deadline`, and flags that record which commands
  were already sent: `budget_requested`, `payment_requested`, `execution_enqueued`).
- Commands go to the `saga-commands` topic with a `target` attribute. Replies come back on
  `billing-events` and `execution-events` and are consumed from `so-saga-replies`.
- Every transition is one database transaction: order status, `saga_instance`, the `outbox` row and,
  for replies, `processed_message`. The relay publishes the outbox rows after commit.
- Replies whose saga does not exist, is not `RUNNING`, or is not in the expected `step` are ignored
  with a log line, because the queue is Standard and the order is not guaranteed.
- A background job checks the `deadline` of each step every 30 seconds. A timed-out `BUDGET` step fails
  the saga and leaves the order in `IN_DIAGNOSIS`, so the attendant can send again. A timed-out payment
  or execution step compensates and cancels the order (`reason=SAGA_TIMEOUT`).
- Compensations are fire-and-forget: the SO marks the saga `COMPENSATED` when it publishes them and
  only logs the confirmations (`BudgetCancelled`, `PaymentRefunded`). The compensation commands are
  idempotent, so resending them is safe.
- The customer's `accept` moves the order to `IN_PROGRESS` synchronously, before any reply from
  Billing. `reject` and `cancel` are synchronous too.

## Consequences

- The whole approval flow is readable in one service and in the transition table of the README.
- Each step has a timeout, so a stuck saga cannot keep an order open forever.
- The SO depends on the message contracts in `docs/fase4/contracts/messaging.md`; a change there must
  be made in every service at the same time.
- Delivery is at least once and unordered, so every consumer needs deduplication by `message_id`
  and the order checks described above. The integration suite must cover the happy path and the
  compensating paths (see `rules/testing.md`).
- The orchestrator is a single point of logic inside the SO. It is the only service that changes
  the saga state, so horizontal scaling relies on the transactions and the deadline job being safe
  when several replicas run at once.
