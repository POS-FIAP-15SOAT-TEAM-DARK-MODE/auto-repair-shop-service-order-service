# Workflow

## Conventional Commits

```
<type>(<scope>): <short description>
```

Types: `feat` · `fix` · `refactor` · `test` · `chore` · `docs` · `perf` · `style` · `revert` · `ci`

Scopes: `service-order` (aggregate, service, controller) · `saga` · `messaging` · `clients` · `domain` · `api` ·
`db` · `k8s` · `compose` · `local` · `docker` · `bootstrap` · `ai`. Also used in the history: `web`, `auth`,
`middleware`, `routing`, `swagger`, `integration`, `deps`, `lint`, `adr`, `readme`.

Rules:
- Subject up to 72 characters, lowercase start, no trailing period, imperative mood.
- Small commits by theme; the body explains why when it is not obvious.
- Breaking changes: `feat(api)!: ...` or a `BREAKING CHANGE:` footer.
- Every commit ends with the trailer required by the team's tooling when it applies.

## Branches and PRs
- Feature branches → PR into `develop` (ADR 0001). `develop` → `main` promotes a release.
- No direct pushes to `main` or `develop`; no force-push.

## Pre-commit Hooks (`.pre-commit-config.yaml`)
- File hygiene: trailing whitespace, end of file, YAML (multi-document allowed), large files, merge conflicts.
- `golangci-lint` (version pinned in the config), `go fmt ./...`, `go build`, `go vet ./...`, `go-unit-tests`.
- `check-ai-rules`: `.ai/` is the only source of agent rules; `.mdc` files are allowed only under `.ai/` and `.cursor/rules/ai-rules-reference.mdc`.

## Make Targets
```bash
make run               # go run cmd/service/main.go
make test              # unit tests with the race detector
make test-integration  # integration tests (Postgres and MiniStack)
make coverage          # coverage profile and total
make up                # docker compose up --build (seeds .env, starts the stubs profile)
make down              # docker compose down -v
make migrate-up        # apply pending migrations
make migrate-down      # roll back one migration
```

## CI (`.github/workflows`)
- `ci.yml` (pull requests): `pre-commit`, `build`, `unit-tests`, `integration-tests`, `coverage`, `sonarqube`.
  The `sonarqube` job needs `SONAR_TOKEN`; it fails without it, which is expected in forks.
- `docker.yml` (push to `develop` = STG, `main` = PRD): build and push the image, then deploy the
  `overlays/{lab,stg,prd}` Kustomize overlay to `deploy/service-order-service`.
