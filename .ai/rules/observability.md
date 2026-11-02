# Observability

## Logger
Structured JSON through Zap (`internal/pkg/logger`). `logger.Of(ctx)` carries the request fields.
Field redaction is centralized in the logger.

## Log Levels
| Level | Use |
|---|---|
| `INFO` | Business events: order created, status transition, saga reply logged or ignored |
| `WARN` | Rejected operations and ignored messages: validation failure, release failed on create or cancel, unexpected deadline status |
| `ERROR` | Unexpected failures: DB errors, outbox or consumer errors, unhandled panics |

## Event Names
Use these names as they are; do not invent new names for the same operation.
- `service_order.created`
- `service_order.status_transition` (fields: `service_order_id`, `from_status`, `to_status`)
- `service_order.validation_failed`
- `service_order.release_reservation_failed`
- `service_order.status_notification_sent` and `service_order.status_notification_failed` (best effort; a failure never fails the transition)
- `saga.started`, `saga.completed`, `saga.compensated`
- `saga.reply_logged`, `saga.reply_ignored` (fields: `type`, `saga_id`)
- `saga.deadline_expired`, `saga.deadlines_expired`, `saga.deadline_unexpected_status`
- `messaging.disabled` (startup, when the queue or the topic variables are empty)
- `middleware.registered` (startup, one line per middleware)

## Metrics
- `GET /metrics` (Prometheus). Every collector is declared and registered in `internal/pkg/metrics`: the HTTP request duration histogram, `service_order_status_transitions_total{status}`, `service_order_notification_failures_total`, `outbox_pending`, `saga_started_total`, `saga_completed_total`, `saga_compensated_total` and `saga_step_duration_seconds{step}`.
- Code increments the counters where the event happens (status change, saga step, notification failure).
- The ServiceMonitor scrapes the `http` port of the service.

## Redaction
Keys `password`, `senha`, `pwd`, `token`, `authorization`, `cpf`, `cnpj`, `document` and `documento` are redacted, and DSN passwords are masked.
Still prefer explicit safe fields over `zap.Any` with domain objects.

## Read Operations
Keep successful reads silent or at debug level. Always log database errors at `ERROR` with the operation name.
