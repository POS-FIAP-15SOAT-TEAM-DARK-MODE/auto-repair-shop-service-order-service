---
trigger: always_on
---

# AI Rules Reference

> **Single source of truth: `.ai/`**
> Only this file is auto-loaded by Windsurf. All linked files must be opened on demand — never loaded automatically.
> Never duplicate rule content here.

## Always-Applied Rules

### 1. Security
- Never log or expose CPF, CNPJ, JWT tokens, the internal token or secrets in any response, log entry, or error message.
- The service validates JWTs only; authentication makes no database call and relies on the JWT claims.
- Protect `/internal/*` with `X-Internal-Token`; never route it through the gateway.
- Validate all input at the controller; never trust raw strings inside services or domain.

### 2. Git Conventions
All commits must follow Conventional Commits: `<type>(<scope>): <description>`.
Types: `feat fix refactor test chore docs perf style revert ci` — scopes in `.ai/rules/workflow.md`.

### 3. Layer Contract (verify after every edit)
- Controllers and routes: zero business logic.
- Services and saga: zero SQL; SQL lives in `repository/` and the saga repository.
- Domain models: zero framework imports (no Gin, no `lib/pq`, no `net/http`).

---

## Task Routing

| Task type | Files to read |
|---|---|
| New feature or domain change | `.ai/rules/architecture.md` · `.ai/rules/domain.md` · `.ai/rules/go-conventions.md` |
| Saga or messaging change | `.ai/rules/domain.md` · `.ai/rules/architecture.md` · `.ai/rules/database.md` · `.ai/rules/testing.md` |
| Database schema or migration | `.ai/rules/database.md` · `.ai/rules/go-conventions.md` |
| API endpoint | `.ai/rules/api.md` · `.ai/rules/architecture.md` · `.ai/rules/security.md` |
| Auth, RBAC, or security | `.ai/rules/security.md` · `.ai/rules/domain.md` |
| Tests | `.ai/rules/testing.md` · `.ai/rules/architecture.md` |
| Logging or observability | `.ai/rules/observability.md` · `.ai/rules/security.md` |
| Git workflow | `.ai/rules/workflow.md` |
| Add a domain aggregate | `.ai/commands/new-domain.md` |
| Add migration | `.ai/commands/add-migration.md` |

All rule files: `.ai/rules/` · All commands: `.ai/commands/` · Index: `.ai/README.md`
