---
name: project-update
description: >-
  Runbook for updating dependencies, toolchains, Docker base images, and GitHub
  Actions in the autoget repository. Use when the user asks to update project
  dependencies, upgrade Go/Node/pnpm/Docker/CI versions, or perform a routine
  dependency audit and bump.
---

# Project Update Runbook

Use this workflow when updating dependencies, runtime toolchains, Docker images,
or GitHub Actions across the `autoget` monorepo (`protocol`, `backend`,
`organizer`, and `frontend`).

Never edit lockfiles (`frontend/pnpm-lock.yaml`, `backend/go.sum`,
`organizer/go.sum`, `go.work.sum`) by hand.

## 1. Go Dependencies (`protocol/`, `backend/`, `organizer/`)

1. **Check current & outdated dependencies** in each module:
   ```bash
   for mod in protocol backend organizer; do
     echo "=== $mod ==="
     (cd "$mod" && go list -u -m -f '{{if and (not .Indirect) .Update}}{{.Path}}: {{.Version}} -> {{.Update.Version}}{{end}}' all)
   done
   ```
2. **Update Go dependencies**:
   - Routine minor/patch/compatible updates across all three modules:
     ```bash
     just update-go-deps
     ```
     (`just update-go-deps` runs `go get -u -t ./...` in `protocol`, `backend`,
     and `organizer`, then runs `just tidy`.)
   - For targeted or major version bumps, run `go get <pkg>@<version>` inside
     the specific module directory, then run `just tidy`.
   - Note: `go.work` is for local development only; Docker builds resolve
     `protocol` and `organizer` via `replace` directives in `backend/go.mod`.
3. **Check for Go vulnerabilities**:
   ```bash
   for mod in protocol backend organizer; do
     (cd "$mod" && go run golang.org/x/vuln/cmd/govulncheck@latest ./...)
   done
   ```
4. **Check Go toolchain version**:
   - Check the `go X.Y.Z` directive in `go.work`, `protocol/go.mod`,
     `backend/go.mod`, and `organizer/go.mod`.
   - Keep the Go version aligned across `Dockerfile`, `Dockerfile.organizer`,
     `.github/workflows/ci.yml`, and `CONTRIBUTING.md`.

## 2. Web Dependencies (`frontend/` with `pnpm`)

Always use `pnpm` (never `npm` or `yarn`).

1. **Check outdated packages**:
   ```bash
   cd frontend && pnpm outdated
   ```
2. **Update dependencies**:
   - Compatible updates within `package.json` ranges:
     ```bash
     just update-pnpm-deps
     ```
   - Major/latest updates (review breaking changes first for `lit`, `vite`,
     `vitest`, `typescript`, `tailwindcss`, `daisyui`, `oxlint`, `oxfmt`, etc.):
     ```bash
     cd frontend && pnpm update --latest <pkg>
     ```
   - Note on `urlpattern-polyfill`: keep it dynamically imported in
     `frontend/src/app.ts` only when `URLPattern` is missing (see
     `CONTRIBUTING.md`).
3. **Audit production dependencies** (matches CI):
   ```bash
   cd frontend && pnpm audit --prod --audit-level=high
   ```
4. **Check Node & pnpm versions**:
   - `frontend/package.json` (`engines.node` and `packageManager`)
   - Keep aligned with `Dockerfile`, `.github/workflows/ci.yml`, and
     `CONTRIBUTING.md`.

## 3. Docker Dependencies & Base Images

Inspect and update both Dockerfiles:

- `Dockerfile`:
  - `FROM node:<ver>-alpine AS frontend-builder` and `npm install -g pnpm@<ver>`
  - `FROM golang:<ver>-alpine AS backend-builder` (and `apk add --no-cache git`)
  - `FROM alpine:<ver> AS deploy` (and `apk --no-cache add ca-certificates tzdata`)
- `Dockerfile.organizer`:
  - `ARG GO_VERSION=<ver>` (`FROM golang:${GO_VERSION}-alpine AS builder`)
  - `ARG ALPINE_VERSION=<ver>` (`FROM alpine:${ALPINE_VERSION} AS deploy`, `apk add --no-cache ca-certificates`)

Checklist:
- Verify the latest stable tags for `node:*-alpine`, `golang:*-alpine`, and
  `alpine:*`.
- Ensure `GO_VERSION`, `NODE_VERSION`, `PNPM_VERSION`, and `ALPINE_VERSION`
  match across `Dockerfile`, `Dockerfile.organizer`, `.github/workflows/ci.yml`,
  `frontend/package.json`, and `CONTRIBUTING.md`.
- Build both images locally to verify:
  ```bash
  just build-images
  ```

## 4. GitHub Actions & CI-Installed Dependencies

Inspect all workflow files under `.github/workflows/` (`ci.yml`, `docker.yml`,
`test-on-pr.yml`) and `.github/dependabot.yml`.

1. **CI-installed tool & runtime versions** (`.github/workflows/ci.yml` `env:`):
   - `GO_VERSION`
   - `GOIMPORTS_VERSION` (`golang.org/x/tools/cmd/goimports@...`)
   - `GOLANGCI_LINT_VERSION`
   - `GOVULNCHECK_VERSION` (`golang.org/x/vuln/cmd/govulncheck@...`)
   - `NODE_VERSION`
   - `PNPM_VERSION`
   - Update `CONTRIBUTING.md` (`## Tooling`) if any of these versions change.
2. **GitHub Action versions** (pinned by commit SHA with a `# vX.Y.Z` comment;
   always update both the commit SHA and the version comment together):
   - `.github/workflows/ci.yml`:
     - `actions/checkout`
     - `actions/setup-go`
     - `golangci/golangci-lint-action`
     - `pnpm/setup`
   - `.github/workflows/docker.yml`:
     - `actions/checkout`
     - `docker/login-action`
     - `docker/metadata-action`
     - `docker/build-push-action`
     - `actions/attest-build-provenance`
   - Check latest action versions and resolve their commit SHAs with `gh`:
     ```bash
     for repo in \
       actions/checkout \
       actions/setup-go \
       golangci/golangci-lint-action \
       pnpm/setup \
       docker/login-action \
       docker/metadata-action \
       docker/build-push-action \
       actions/attest-build-provenance
     do
       tag=$(gh api "repos/$repo/releases/latest" --jq '.tag_name')
       obj_type=$(gh api "repos/$repo/git/ref/tags/$tag" --jq '.object.type')
       sha=$(gh api "repos/$repo/git/ref/tags/$tag" --jq '.object.sha')
       if [ "$obj_type" = "tag" ]; then
         sha=$(gh api "repos/$repo/git/tags/$sha" --jq '.object.sha')
       fi
       echo "$repo: $sha # $tag"
     done
     ```

## 5. Verification

Run the full local suite after all updates:

```bash
just fmt lint build test
```

If Dockerfiles were modified, also run:

```bash
just build-images
```

Check `git status` and `git diff` to verify only expected files changed, and
commit using conventional commits (e.g., `build(deps): ...` or `ci: ...`).
