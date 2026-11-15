# auto-repair-shop-service-order-service

Serviço de **Ordem de Serviço** da Auto Repair Shop (porta `8086`, banco `service_order`).
É dono do agregado `ServiceOrder` e **orquestra a saga de aprovação** do orçamento: a OS
pede o orçamento ao Billing, espera a decisão do cliente, pede o pagamento e enfileira a
execução no Execution, compensando quando algum passo falha.

## Máquina de estados da OS

```
NEW → RECEIVED → IN_DIAGNOSIS → AWAITING_APPROVAL → IN_PROGRESS → COMPLETED → DELIVERED
                                        │                 │
                                        ▼                 ▼
                                    REJECTED          CANCELLED
```

- `AWAITING_APPROVAL` existe só enquanto o cliente não decidiu.
- No `accept` a OS vai **imediatamente** a `IN_PROGRESS`; pagamento e execução seguem sem mudar o status.
- `IN_PROGRESS → REJECTED` (pagamento recusado) e `IN_PROGRESS → CANCELLED` (falha de execução ou timeout)
  só acontecem pela saga.
- Terminais: `DELIVERED`, `REJECTED` (`reason`: `BUDGET_REJECTED` ou `PAYMENT_REJECTED`) e `CANCELLED`
  (`reason`: `USER_CANCELLED`, `EXECUTION_FAILED` ou `SAGA_TIMEOUT`).
- Cancelável: qualquer status anterior a `COMPLETED`, exceto os terminais.

Transições locais (sem saga): `received` (NEW→RECEIVED), `start-diagnosis` (RECEIVED→IN_DIAGNOSIS),
`deliver` (COMPLETED→DELIVERED). Toda mudança de status é gravada com o evento
`ServiceOrderStatusChanged` na mesma transação (outbox) e notifica o `StatusNotifier` após o commit.

## Saga `service-order-approval`

Disparada por `PUT /v1/service-order/{id}/send` (OS em `IN_DIAGNOSIS`, com ao menos um work).
O estado fica em `saga_instance` (`step`, `status`, `deadline` e flags `budget_requested`,
`payment_requested`, `execution_enqueued`).

| Gatilho | Efeito |
|---|---|
| `send` | saga `step=BUDGET`; comando `GenerateBudget`. OS segue `IN_DIAGNOSIS` |
| `BudgetGenerated` | OS → `AWAITING_APPROVAL`; saga `AWAITING_CUSTOMER` (sem deadline) |
| `accept` (cliente) | OS → `IN_PROGRESS`; saga `PAYMENT`; comando `RequestPayment` |
| `reject` (cliente) | OS → `REJECTED` (`BUDGET_REJECTED`); `CancelBudget` + `ReleaseStock`; saga `COMPENSATED` |
| `PaymentApproved` | saga `EXECUTION_QUEUE`; comando `EnqueueExecution` |
| `PaymentFailed` | OS → `REJECTED` (`PAYMENT_REJECTED`); `CancelBudget` + `ReleaseStock`; saga `COMPENSATED` |
| `ExecutionQueued` | saga `EXECUTION` |
| `ExecutionCompleted` | OS → `COMPLETED`; saga `COMPLETED` |
| `ExecutionFailed` | OS → `CANCELLED` (`EXECUTION_FAILED`); `RefundPayment` + `CancelBudget` + `ReleaseStock`; saga `COMPENSATED` |
| `cancel` (usuário) | OS → `CANCELLED` (`USER_CANCELLED`); compensa o que existe: `CancelExecution`, `RefundPayment`, `CancelBudget`, e `ReleaseStock` se houver supplies |
| Job de prazos (30 s) | `BUDGET` vencido: saga `FAILED`, `CancelBudget`, OS segue `IN_DIAGNOSIS`. `PAYMENT` ou `EXECUTION_QUEUE` vencido: compensa como no `cancel`, OS → `CANCELLED` (`SAGA_TIMEOUT`) |

Compensações são fire-and-forget: a saga é marcada `COMPENSATED` ao publicar os comandos. As
confirmações `BudgetCancelled` e `PaymentRefunded` são só registradas em log.
Mensagens cujo `saga_id` não existe, cuja saga não está `RUNNING` ou cujo `step` não é o esperado
são ignoradas com log.

Prazos: `SAGA_STEP_TIMEOUT` (padrão `2m`) e `SAGA_PAYMENT_TIMEOUT` (padrão `24h`).

## Snapshots

A OS guarda cópias dos dados de outros serviços, para não consultá-los nas leituras
(ADR `0003-service-order-snapshots`):

- **Cliente**: `id, user_id, name, email, type, document, company_name, phone`.
- **Veículo**: `id, license_plate, brand, model, year`.
- **Works**: `name`, `description`, `unit_price` (buscados no Catalog ao adicionar).
- **Supplies**: `name`, `description`, `unit_price`, `version`, `quantity`, `reservation_id`.

`total_amount = Σ work.unit_price + Σ supply.unit_price × quantity`, recalculado de forma síncrona
na mesma transação a cada inclusão ou remoção.

## Dependências

| Serviço | Uso | Variável |
|---|---|---|
| Customer | `GET /internal/customers/{id}`, `GET /internal/vehicles/{id}` | `CUSTOMER_URL` |
| Catalog | `GET /internal/works/{id}`, `POST`/`DELETE /internal/reservations` | `CATALOG_URL` |
| Billing, Execution | via mensageria (comandos e eventos abaixo) | ver `.env.example` |

Chamadas REST usam `X-Internal-Token` (`INTERNAL_TOKEN`), timeout de 3 s, sem retry em `POST`.
Falha de upstream retorna `502`.

## Mensageria

| Direção | Recurso | Conteúdo |
|---|---|---|
| publica | `saga-commands` (atributo `target`) | `GenerateBudget`, `CancelBudget` (billing); `RequestPayment`, `RefundPayment` (billing); `EnqueueExecution`, `CancelExecution` (execution); `ReleaseStock` (catalog) |
| publica | `so-events` | `ServiceOrderStatusChanged` |
| consome | `so-saga-replies` | `BudgetGenerated`, `PaymentApproved`, `PaymentFailed`, `ExecutionQueued`, `ExecutionCompleted`, `ExecutionFailed`; `BudgetCancelled`, `PaymentRefunded` e `ExecutionWorkStatusChanged` só geram log |

Envelope JSON com `message_id` (chave de idempotência), `saga_id`, `type`, `occurred_at`,
`service_order_id`, `request_id` e `payload`. Contratos completos em
`docs/fase4/contracts/messaging.md` (repositório `auto-repair-shop`).

Publicação via outbox e relay; consumo com deduplicação em `processed_message`.
A entrega é *at-least-once* e sem ordem garantida, por isso a saga valida o estado antes de agir.

## Endpoints

A documentação completa está em `docs/swagger.yaml` (servida em `/swagger/index.html`).

| Método e rota | Papel |
|---|---|
| `POST /v1/service-order` | attendant, admin |
| `GET /v1/service-order`, `GET /v1/service-order/{id}`, `GET /v1/service-order/{id}/works`, `GET /v1/service-order/{id}/supplies` | attendant, mechanic, admin |
| `GET /v1/service-order/{id}/status` | público (sem JWT) |
| `POST`/`DELETE /v1/service-order/{id}/works[/{serviceId}]` | attendant (só `NEW`) |
| `POST`/`DELETE /v1/service-order/{id}/supplies[/{supplyId}]` | mechanic (só `IN_DIAGNOSIS`) |
| `PUT /v1/service-order/{id}/received` | attendant |
| `PUT /v1/service-order/{id}/start-diagnosis` | mechanic |
| `PUT /v1/service-order/{id}/send` | mechanic (`409` se não estiver em `IN_DIAGNOSIS` ou houver saga em curso) |
| `PUT /v1/service-order/{id}/accept`, `.../reject` | customer, admin (`409` se não estiver em `AWAITING_APPROVAL` ou a saga não aguardar o cliente) |
| `PUT /v1/service-order/{id}/cancel` | attendant, mechanic, admin |
| `PUT /v1/service-order/{id}/deliver` | attendant |
| `GET /ping`, `GET /metrics` | operação |

Rota interna (não roteada pelo gateway): `GET /internal/service-orders/exists?customer_id=|vehicle_id=`,
com `X-Internal-Token`. Responde `{"exists": bool}`.

## Executar localmente

Pré-requisitos: Docker e Docker Compose. Não há dependência de Go para subir a stack.

```bash
make up        # cria .env a partir de .env.example (se faltar), sobe app, db, migrations, MiniStack e stubs
curl -fsS http://localhost:8086/ping
make down      # derruba a stack e apaga volumes
```

O `make up` ativa o profile `stubs` (WireMock de Customer e Catalog em `local/stubs/`).
Sem ele, as chamadas REST para Customer e Catalog falham com `502`.
`local/init-aws.sh` cria, no MiniStack, os tópicos e filas deste serviço e as filas de teste
`test-{billing,execution,catalog}-commands`, que a suíte de integração usa para simular Billing e Execution.

Outros alvos: `make test`, `make test-integration` (exige Postgres e MiniStack), `make coverage`,
`make run` (sem Docker, exige um Postgres e as variáveis do `.env`).

## Testes e cobertura

```bash
make test              # testes unitários com o race detector (LOG_LEVEL=PANIC)
make test-integration  # integração: exige Postgres e MiniStack (ou o job da CI)
make coverage          # perfil de cobertura (coverage.out) e o total
```

Medição de 2026-11-01 com `go test ./... -count=1 -cover`. O repositório não gera mocks, então o total
é o total de todos os pacotes (o filtro `/mocks/` do CI continua válido e não remove nada).

| Escopo | Medido | Gate da CI |
|---|---|---|
| Total (statements) | **93.3%** | ≥ 80% |
| `internal/saga` | 99.0% | ≥ 90% |
| `internal/service_order/domain` | 100.0% | ≥ 90% |

Pacotes abaixo de 90%: `internal/app` (36.4%: a montagem em `bootstrap` e a conexão com o banco não têm
teste unitário), `internal/clients` (85.5%) e `internal/pkg/db` (84.2%). Não têm teste unitário:
`cmd/service`, `internal/app/bootstrap` e `internal/pkg/metrics`. Os testes de integração (`-tags=integration`)
não entram nesses números; rodam na CI.

## Configuração

Variáveis em `.env.example`. Os segredos (`POSTGRES_*`, `JWT_SECRET`, `INTERNAL_TOKEN`) vêm do
Secrets Manager (`auto-repair-shop-<env>/service-order`) nos ambientes do Kubernetes.

## Estrutura

- `internal/service_order/`: domínio (`domain/`), casos de uso (`service.go`), HTTP (`controller.go`, `routes.go`), repositórios Postgres e em memória.
- `internal/saga/`: orquestrador, máquina da saga, job de prazos e repositório da saga.
- `internal/clients/`: clientes REST de Customer e Catalog.
- `internal/pkg/messaging/`: outbox, relay SNS, consumidor SQS.
- `migrations/`: schema do banco `service_order`.
- `k8s/manifests/`: base Kustomize e overlays `lab`, `stg`, `prd`.
- `docs/adr/`: decisões de arquitetura.
