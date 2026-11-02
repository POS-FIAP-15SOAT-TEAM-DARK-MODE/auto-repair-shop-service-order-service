# Security

## JWT
- Protected routes require `Authorization: Bearer <JWT>` (HS256, `JWT_SECRET`, shared with the other services). Missing or invalid token: `401`.
- Public routes: `GET /ping`, `GET /metrics`, `GET /v1/service-order/:id/status`.
- The middleware reads the role and the user id from the claims. It makes no database call to authenticate.
- Customer checks (`accept`, `reject`) compare the claim `user_id` with the snapshot `customer_user_id` of the order.

## RBAC Enforcement
- Roles come from `internal/pkg/auth/roles.go`. Route groups are declared in `internal/service_order/routes.go`.
- Out-of-scope role: `403`. Missing token: `401`.
- Do not add a route without a role group. A public route must be listed in this file and in `api.md`.

## Internal Routes
- `/internal/*` requires the header `X-Internal-Token` equal to `INTERNAL_TOKEN`, compared in constant time.
- An empty configured token rejects every request (fail closed).
- The gateway never routes `/internal/*`; the ingress must not expose them either.
- Outgoing calls to Customer and Catalog send the same header and propagate `X-Request-Id`.

## Personal Data
- The order stores copies of CPF or CNPJ (`customer_document`), name, e-mail and phone for its own flow.
  They are never written to logs, error messages or responses other than the detail endpoint that already returns them.
- Log only ids. Field keys `password`, `senha`, `pwd`, `token`, `authorization`, `cpf`, `cnpj`, `document` and `documento` are redacted by `internal/pkg/logger`; DSN passwords in strings are masked.
- Notifications log a masked recipient (`service_order.status_notification_sent`).
- Use explicit fields (`zap.String("service_order_id", id)`); do not pass whole snapshots or request bodies to the logger.

## Secrets
- Secrets (`POSTGRES_PASSWORD`, `JWT_SECRET`, `INTERNAL_TOKEN`) come from Secrets Manager (`auto-repair-shop-<env>/service-order`) in the clusters, and from `.env` locally. Never commit a real value; `.env.example` holds placeholders only.

## Input Sanitization
- Validate at the controller: ids, lists, amounts. Services and domain do not trust raw strings.
- Never expose stack traces or raw database errors. Upstream errors become the mapped statuses above.
