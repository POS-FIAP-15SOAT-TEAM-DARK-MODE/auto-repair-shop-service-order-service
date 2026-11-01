# Testing

## Coverage Gates (`.github/workflows/ci.yml`, job `coverage`)
- Total coverage ≥ **80%**, computed without generated mocks (`/mocks/` lines are removed from the profile).
- `internal/saga` and `internal/service_order/domain` ≥ **90%** each.
- `sonar.exclusions` also excludes mocks and `migrations/`, `docs/`, `cmd/`.

## Commands
```bash
make test              # go test ./... --race (LOG_LEVEL=PANIC)
make test-integration  # go test -tags=integration ./internal/integration/... (needs Postgres and MiniStack)
make coverage          # coverage.out and the total
go test ./internal/saga/... -run TestOrchestrator -v
```
Without a local Docker daemon, verify with `go build ./...`, `go vet ./...` (also with `-tags=integration`)
and `go test --race`; the integration tests run in CI.

## Unit Tests
- Repositories: `repository.NewInMemory()` (`repository/so_in_memory.go`), `saga.NewMemoryRepository()` and `saga.NewMemoryOutbox()` (`saga/memory.go`) implement the same interfaces as the Postgres versions.
- UoW: `uow.NewInMemoryUoW()` for service tests (`internal/pkg/uow/in_memory.go`).
- Mocks: mockery, generated into `mocks/` folders; used at interface boundaries only.
- Clients (`internal/clients`): `httptest` servers; cover 404, 409, 422, 5xx, timeout and header propagation.

## Integration Tests (`-tags=integration`)
- Real Postgres (`service_order` database) and MiniStack (queues from `local/init-aws.sh`).
- The test plays Billing and Execution: it reads commands from `test-{billing,execution,catalog}-commands` and publishes replies on `billing-events` and `execution-events`.
- Customer and Catalog are simulated with `httptest`.
- Each test cleans its rows; tests do not depend on each other.
- Required scenarios: happy path (`send → BudgetGenerated → accept → PaymentApproved → ExecutionQueued → ExecutionCompleted`, with `IN_PROGRESS` right after `accept`); `PaymentFailed` → `REJECTED`; `ExecutionFailed` → `CANCELLED`; `reject` → `REJECTED`; duplicate message → one effect; every status change publishes `ServiceOrderStatusChanged`.

## Structure Rules
- Arrange / Act / Assert. Tests are independent.
- Name: `Test<Unit>_<Condition>_<ExpectedOutcome>`; group cases with `t.Run`.
- Contract fixtures: `testdata/contracts/<Type>.json` for every command published and every event consumed.

## What to Cover
| Area | Critical cases |
|---|---|
| Status transitions | Each guard violated (`409`), each allowed transition, terminal states |
| Saga | Each row of the transition table, each compensation set per step, deadline job for BUDGET and PAYMENT, out-of-order replies ignored |
| Pricing | Recalculated on add and remove, same transaction |
| Snapshots | Copied values are kept when the source changes |
| RBAC | `403` for a wrong role, customer on another order, `401` without token |
| Internal check | `401` without token, `422` with zero or two parameters |
