# Observability

## Logger
Structured JSON through Zap (`internal/pkg/logger`). `logger.Of(ctx)` carries the request fields.
Field redaction is centralized in the logger.

## Log Levels
| Level | Use |
|---|---|
| `INFO` | Business events: order created, status transition, saga reply logged or ignored |
| `WARN` | Rejected operations and ignored messages: validation failure, release failed, unexpected deadline status |
| `ERROR` | Unexpected failures: DB errors, outbox or consumer errors, unhandled panics |

## Event Names
Use these names as they are; do not invent new names for the same operation.
- `service_order.created`
- `service_order.status_transition` (fields: `service_order_id`, `from_status`, `to_status`)
- `service_order.validation_failed`
- `service_order.release_reservation_failed`
- `service_order.status_notification_sent` and `service_order.status_notification_failed` (best effort; a failure never fails the transition)
- `saga.reply_logged`, `saga.reply_ignored` (fields: `type`, `saga_id`)
- `saga.deadline_unexpected_status`

## Metrics
- `GET /metrics` (Prometheus). The HTTP request duration histogram is the only shared collector (`internal/pkg/metrics`).
- Order-specific counters (for example, status transitions and notification failures) are registered where the transition happens.
- The ServiceMonitor scrapes the `http` port of the service.

## Redaction
Keys `password`, `token`, `authorization`, `cpf`, `cnpj`, `document` and `documento` are redacted, and DSN passwords are masked.
Still prefer explicit safe fields over `zap.Any` with domain objects.

## Read Operations
Keep successful reads silent or at debug level. Always log database errors at `ERROR` with the operation name.
