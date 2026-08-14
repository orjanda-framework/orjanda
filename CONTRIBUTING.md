# Contributing to Orjanda

First off: thank you for considering a contribution. Orjanda is an open-source,
agent-native business application framework written in Go, and it only gets
better with outside help.

Please read the [Code of Conduct](CODE_OF_CONDUCT.md) — everyone participating
in this project is expected to follow it.

## Table of contents

- [Development setup](#development-setup)
- [Build and test commands](#build-and-test-commands)
- [Formatting and linting](#formatting-and-linting)
- [Branch and PR workflow](#branch-and-pr-workflow)
- [Commit messages](#commit-messages)
- [What we expect from a contribution](#what-we-expect-from-a-contribution)
- [Architecture guidelines](#architecture-guidelines)

---

## Development setup

### Prerequisites

- **Go 1.26+** — see `go.mod`. No CGo is required: the SQLite dialect uses
  `modernc.org/sqlite` (pure Go).
- **Node.js 22+** and **npm** — required only when working on the admin UI in
  `orjanda-ui/`.
- **Docker** — required only to run the PostgreSQL integration tests
  (testcontainers-go). Unit tests never need Docker.
- **golangci-lint** — version compatible with the `version: "2"` config in
  `.golangci.yml` (the CI action uses `latest`).

### Clone and build

```bash
git clone https://github.com/orjanda-framework/orjanda.git
cd orjanda
go build ./...
```

> Orjanda is a framework. To exercise it as a user would — scaffolding an
> application with the CLI, adding Documents, running `orjanda serve` — install
> the CLI or use it from your checkout: `go run ./cmd/orjanda init myapp`, then
> `cd myapp && go run <path-to-checkout>/cmd/orjanda serve`. See
> [README](README.md#getting-started).

### Project layout

The framework is a modular monolith. Packages live in the layout fixed by
TAD §5.1; do not rename or restructure it:

```
errors/ config/ app/ schema/ dal/ cache/ search/ background/
document/ event/ perm/ workflow/ audit/ auth/ orjanda-core/
api/ agent/ ui/ server/ cli/ cmd/orjanda/ testing/ orjanda-ui/
```

The authoritative specifications are:

| File | Use it to... |
|---|---|
| `docs/ORJANDA-PRD.md` | Understand *why* a behavior exists before changing it |
| `docs/ORJANDA-TAD.md` | Get the *exact* interface, type, or algorithm before writing code |

**Rule:** never invent an interface, package name, or field the TAD does not
already specify. If the docs are silent on something you need, ask in the PR or
an issue rather than improvising new architecture.

---

## Build and test commands

The exact commands below are what CI runs. **Make sure they all pass before you
open a PR** (the integration and frontend lanes only where applicable).

### Go build, vet, and unit tests

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
```

### Integration tests (PostgreSQL via testcontainers)

```bash
go test -tags integration -count=1 ./testing/...
```

These require Docker and a working container runtime. They are gated behind the
`integration` build tag so the fast unit lane never depends on Docker.

### Linting

```bash
golangci-lint run
```

### Admin UI (`orjanda-ui/`)

```bash
cd orjanda-ui
npm ci
npm run typecheck     # tsc --noEmit
npm test              # vitest
npm run build         # tsc --noEmit && vite build
```

`npm run build` overwrites the committed `orjanda-ui/dist/` that the Go binary
embeds via `embed.FS` — if you change the UI, the rebuilt `dist` must be part of
your PR so `go build ./...` still embeds a fresh build.

---

## Formatting and linting

- **gofmt** must be satisfied. golangci-lint's config enables the `gofmt`
  formatter; check with `gofmt -l .` before committing.
- golangci-lint runs with the linters configured in `.golangci.yml`
  (errcheck, govet, ineffassign, staticcheck, unused, gocritic, misspell,
  revive, noctx, bodyclose).
- Comment formatting/spelling errors caught by `misspell` and `revive` will fail
  CI — run `golangci-lint run` locally to catch them early.

---

## Branch and PR workflow

1. **Fork** the repository and create a feature branch from `main`:
   ```bash
   git checkout -b feat/my-descriptive-branch-name
   ```
2. Make your changes, adding tests for new or changed behavior.
3. Run the relevant [build/test commands](#build-and-test-commands) locally.
4. Commit with a [conventional message](#commit-messages) and push to your fork.
5. Open a **pull request against `main`**. CI runs build, vet, unit tests with
   `-race`, golangci-lint, the integration lane, and the frontend lane on every
   PR. The PR must pass CI before merge.
6. Address review feedback by pushing additional commits to the same branch.

Keep PRs small and focused — one logical change per PR makes review faster and
landing easier.

---

## Commit messages

Orjanda follows conventional commit prefixes. The existing history uses
`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, and `ci:`. Keep the summary
line imperative and under ~72 characters:

```
feat(perm): enforce role checks for custom tool execution

Explain what and why — the body is not optional when the change is
non-trivial. Reference PRD/TAD sections instead of restating them,
e.g. "see TAD §8.1 step 3".
```

---

## What we expect from a contribution

- **Tests.** New or changed behavior ships with tests. Bug fixes should add a
  regression test that fails without the fix. Prefer the
  [`orjanda/testing`](testing/) harness over ad hoc fixtures.
- **One permission path.** Every read/write — REST, RPC, agent tool, workflow
  transition — must go through `perm.Engine` (PRD §25.1). A hand-rolled
  permission check outside the engine is a defect, not a shortcut.
- **Errors use the framework error model.** Every fallible exported function
  returns or wraps an `errors.Error` with one of the six `ErrorCode`s
  (TAD §1.1). Don't introduce a parallel error type for a condition the enum
  already covers.
- **Context, not globals.** `auth.Identity`, `tenant_id`, and `request_id`
  travel only via `context.Context` (TAD §1.2).
- **No secrets.** Never commit credentials, `.env` files, or real API keys.
  Secrets belong in `ORJANDA_*` environment variables.
- **Respect the boundaries.** Do not implement post-MVP scope: no microservices,
  no runtime plugin loading, no activation of multi-tenancy, no MySQL dialect,
  no MCP server, no removing the agent approval gates (see PRD §5 and §44.3).

---

## Architecture guidelines

- **Follow the package DAG (TAD §5.1).** Packages build bottom-up; higher layers
  depend on lower ones. If you are about to create a package not in the layout
  above, stop and check the TAD — it is probably meant to live inside one of the
  existing packages.
- **Extension points.** New extension interfaces are the exception, not the
  rule. TAD §9 fixes the extension-point surface (search, cache, background,
  ui, auth, llm, and friends); check the resolution table before adding an
  eleventh kind.
- **Code-first schemas only.** Documents are Go structs compiled at startup.
  There is deliberately no runtime-editable schema metadata and no schema-editing
  UI (PRD §8.4). Don't add one.
- **Cite, don't copy.** Reference PRD/TAD section numbers in comments and commit
  messages instead of re-explaining the rationale at length.
- **Transactional audit.** Every Document/Workflow Engine write and its
  `audit.Entry` share one `dal.Tx`; a failed audit write rolls back the data
  write (TAD §13.1). Keep it that way.

---

## Getting help

- **Questions** — open a GitHub Discussion (once enabled) or an issue.
- **Bugs** — use the bug report issue template.
- **Feature ideas** — use the feature request issue template.
- **Security vulnerabilities** — do **not** open an issue. Follow
  [SECURITY.md](SECURITY.md) and report via a private GitHub security advisory.

Thanks for contributing to Orjanda!
