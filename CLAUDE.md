# CLAUDE.md

See [CONTRIBUTING.md](CONTRIBUTING.md) for layout, commands, tests and commit
conventions, and [organizer/AGENTS.md](organizer/AGENTS.md) for organizer
design rules.

Key rules:

- Use the root `Justfile` recipes (`just fmt lint build test`); all must pass.
- `just build` compiles only `backend/cmd/main.go`; keep package-main code there.
- Never edit lockfiles (`pnpm-lock.yaml`, `go.sum`, `go.work.sum`) by hand.
- Update `backend/openapi.yaml` whenever API routes change.
- The backend `auth` config section is required (or `auth: {disabled: true}`).
- One feature per commit, conventional commit messages.
- Do not over-design; choose the simplest implementation that works.
