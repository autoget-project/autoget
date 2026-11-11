# Contributing

## Layout

The repo is a monorepo of three Go modules and a frontend:

- `protocol/` - shared types between the backend and the organizer
- `backend/` - Go/gin API server, indexers, downloaders; serves the frontend
- `organizer/` - post-processing service (see `organizer/AGENTS.md`)
- `frontend/` - Lit + TypeScript web UI (pnpm)

`go.work` is for local development only. Docker builds do not use it: the
backend resolves `protocol` and `organizer` through `replace` directives in
`backend/go.mod`. `go.work.sum` is gitignored. The `go 1.27.1` line in
`go.work`/`go.mod` is a minimum version; CI and the Dockerfile use Go 1.27.4.

Go import prefix is `github.com/autoget-project/autoget/<module>`; `just fmt`
runs `goimports -local` with that prefix per module.

## Tooling

Versions used in CI (`.github/workflows/ci.yml`): Go 1.27.4, Node 26, pnpm
12.18.0, goimports v0.49.0, golangci-lint v2.15.0, govulncheck v1.9.0.

## Commands

Run from the repo root (see the `Justfile` for all recipes):

- `just fmt` - format all modules (`just fmt-check` only checks)
- `just lint` - golangci-lint per Go module, frontend oxlint
  (`--deny-warnings`) and frontend typecheck
- `just typecheck` - frontend TypeScript check only
- `just build` - backend, organizer and frontend. The backend build compiles
  only `backend/cmd/main.go`, so code in package main must live in that file.
- `just test` - all unit tests (`just test-coverage` reports coverage)

Run `just fmt lint build test` before committing.

## Tests

- Go tests use `stretchr/testify` (`require` for setup, `assert` for results);
  helpers call `t.Helper()`. The SQLite test helper is
  `backend/internal/db/dbtest`.
- Tests that hit live indexer sites are skipped unless `LOCAL_TEST` is set;
  `just test-backend` sets it. CI runners are often blocked by those sites.
- Organizer e2e tests (`just test-e2e`) call live LLM providers and cost money;
  run only the specific case you are working on.

## Backend notes

- The `auth` config section is required. Configure OIDC, or explicitly set
  `auth: {disabled: true}` (unauthenticated API; only behind other access
  control). Local dev configs need one or the other.
- Any route change under `/api/v1` or `/auth` must update
  `backend/openapi.yaml`; `backend/cmd/openapi_test.go` fails otherwise.
- API JSON uses snake_case field names (e.g. `DownloadStatus`).
- Schema management is GORM `AutoMigrate` only; it never renames or drops
  columns. See "Upgrading and database migrations" in `README.md` before changing models.

## Frontend notes

- Never edit `pnpm-lock.yaml` by hand; use `pnpm`. Node `>=26` is required.
- `@lit-labs/router` (labs, 0.1.x) and `urlpattern-polyfill` are a known
  dependency risk; Dependabot surfaces updates for them.
- `urlpattern-polyfill` is loaded on demand (dynamic import in `src/app.ts`)
  only when `URLPattern` is missing, and the router module loads after it.
  Keep it out of static imports so modern browsers never download it.

## Commits

One feature or fix per commit, in conventional commit style with a scope as in
`git log`, e.g. `feat(auth): ...`, `fix(transmission): ...`,
`refactor(frontend): ...`, `docs: ...`, `ci: ...`, `chore: ...`,
`build(deps): ...`. Add a short body explaining why.
