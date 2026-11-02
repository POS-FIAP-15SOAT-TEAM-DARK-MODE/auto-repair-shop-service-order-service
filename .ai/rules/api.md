# API

## Versioning and Routing
Public routes live under `/v1`. The gateway forwards them. `/internal/*` is never routed by the gateway.
Swagger UI: `http://localhost:8086/swagger/index.html`. The spec is `docs/swagger.yaml`, embedded in the binary (`docs/embed.go`).

## Endpoints

```
POST   /v1/service-order                           ADMIN, ATTENDANT
GET    /v1/service-order                           ADMIN, ATTENDANT, MECHANIC
GET    /v1/service-order/:id                       ADMIN, ATTENDANT, MECHANIC
GET    /v1/service-order/:id/status                public (no JWT)
POST   /v1/service-order/:id/works                 ADMIN, ATTENDANT              (status NEW)
GET    /v1/service-order/:id/works                 ADMIN, ATTENDANT, MECHANIC
DELETE /v1/service-order/:id/works/:serviceId      ADMIN, ATTENDANT              (status NEW)
POST   /v1/service-order/:id/supplies              ADMIN, MECHANIC               (status IN_DIAGNOSIS)
GET    /v1/service-order/:id/supplies              ADMIN, ATTENDANT, MECHANIC
DELETE /v1/service-order/:id/supplies/:supplyId    ADMIN, MECHANIC               (status IN_DIAGNOSIS)
PUT    /v1/service-order/:id/received              ADMIN, ATTENDANT
PUT    /v1/service-order/:id/start-diagnosis       ADMIN, MECHANIC
PUT    /v1/service-order/:id/send                  ADMIN, MECHANIC               (IN_DIAGNOSIS, no saga running)
PUT    /v1/service-order/:id/accept                ADMIN, CUSTOMER               (AWAITING_APPROVAL; caller is the customer)
PUT    /v1/service-order/:id/reject                ADMIN, CUSTOMER               (AWAITING_APPROVAL; caller is the customer)
PUT    /v1/service-order/:id/cancel                ADMIN, ATTENDANT, MECHANIC    (cancelable status)
PUT    /v1/service-order/:id/deliver               ADMIN, ATTENDANT              (COMPLETED)
GET    /ping, GET /metrics                         operations
```

Internal (not in Swagger, not routed by the gateway):
```
GET    /internal/service-orders/exists?customer_id= | ?vehicle_id=     X-Internal-Token
```

## Request and Response Conventions
- JSON bodies. Request fields: `client`, `vehicle`, `works`, `services`, `supplies` (`id`, `amount`).
- Response fields are camelCase (`totalValue`, `companyName`, `licensePlate`, `brandModel`).
- Money: `totalValue` is a number in list and detail responses; work and supply prices are decimal strings.
- Timestamps: UTC, RFC 3339.
- Ids: ULID for orders, UUID strings for customers, vehicles, works and supplies.
- Lists: `page` (1-based), `pageSize`, plus `status`, `customerId`, `vehicleId`, `sort_by`, `sort_order`.

## Error Format
```json
{ "code": 409, "errors": ["service order is not in NEW status"] }
```
`errors` (list) is used only for 400 validation. Every other status answers `error` (single message): domain messages, or the fixed `conflict`, `invalid data`, `internal server error` for infrastructure failures. Bodies never contain causes, driver messages, CPF, CNPJ, tokens or stack traces; the full cause goes to the server log.

| HTTP | Meaning in this service |
|---|---|
| 400 | Malformed body, empty id, empty service list |
| 401 | Missing or invalid JWT, or missing or wrong `X-Internal-Token` |
| 403 | Role not allowed, or a customer acting on another customer's order |
| 404 | Order, customer, vehicle, work, supply or reservation not found |
| 409 | Status transition not allowed, or a saga is already running or not waiting |
| 422 | Business rule violation (for example, out of stock, a Catalog validation answer, or the internal check without exactly one parameter) |
| 502 | Customer or Catalog unavailable, or an unexpected upstream answer |
| 500 | Unexpected error |

## Swagger
Every operation has summary, description, request and response schemas, and the error statuses it can
return. Protected operations document 401 and 403; a status guard (409) or a Catalog call (422, 502) is
documented where the handler can return it. `ErrorResponse` (`code`, `error`) and
`ValidationErrorResponse` (`code`, `errors`, 400 only) are the two error schemas.
`/metrics` and `/ping` are documented; the Swagger UI routes and `/internal/*` are not.
Update `docs/swagger.yaml` in the same commit as any route change.
