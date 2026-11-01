# AI Rules — Master Index

Single source of truth for all AI agent rules in this project (service-order-service).
Agent pointer files (`CLAUDE.md`, `AGENTS.md`, `.cursor/`, `.windsurf/`) only contain a routing
table and the always-applied rules. Never duplicate rule content in them.

## Folder Map

```
.ai/
  rules/               # topic-scoped rule files — open on demand
    architecture.md        layers, UoW, saga orchestrator, repository connection pattern
    domain.md              ubiquitous language, status lifecycle, saga rules, RBAC, snapshots
    go-conventions.md      Go idioms, decimal, ULID/UUID, no framework leakage, mockery
    database.md            PostgreSQL conventions, migrations, outbox/processed_message
    api.md                 endpoints with roles, error format, Swagger
    testing.md             coverage gates, unit vs integration patterns
    security.md            JWT, RBAC enforcement, internal token, PII
    observability.md       Zap, log levels, event names, redaction
    workflow.md            Conventional Commits, pre-commit hooks, make targets
  commands/            # reusable playbooks
    new-domain.md          add a domain aggregate end-to-end
    add-migration.md       create and apply a versioned migration
  hooks/               # pre-commit hook scripts
    check-ai-rules.sh      enforces .ai/ as the only rule source
```

## Topic Index

| Topic | File | Key contents |
|---|---|---|
| Architecture | `rules/architecture.md` | Layers, UoW, saga orchestrator, DI in bootstrap |
| Domain & Business Rules | `rules/domain.md` | Status lifecycle, saga, roles, snapshots, total formula |
| Go Conventions | `rules/go-conventions.md` | Stack, IDs, decimal, no framework leakage, mockery |
| Database | `rules/database.md` | NUMERIC(10,2), migrations, saga tables, indexes |
| API | `rules/api.md` | Public endpoints with roles, error format, Swagger |
| Testing | `rules/testing.md` | 80% total / 90% saga and domain, unit and integration |
| Security | `rules/security.md` | JWT, RBAC, internal token, PII |
| Observability | `rules/observability.md` | Zap, log levels, event names, redaction |
| Workflow | `rules/workflow.md` | Conventional Commits, pre-commit, make targets |

## Task Routing — Required Reading by Task Type

| Task type | Files to read |
|---|---|
| New feature or domain change | `architecture.md` · `domain.md` · `go-conventions.md` |
| Saga or messaging change | `domain.md` · `architecture.md` · `database.md` · `testing.md` |
| Database schema or migration | `database.md` · `go-conventions.md` |
| API endpoint | `api.md` · `architecture.md` · `security.md` |
| Auth, RBAC, or security | `security.md` · `domain.md` |
| Tests | `testing.md` · `architecture.md` |
| Logging or observability | `observability.md` · `security.md` |
| Git workflow | `workflow.md` |
| Scaffold a new domain (command) | `commands/new-domain.md` |
| Add migration (command) | `commands/add-migration.md` |
