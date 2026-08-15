# Developing the Orjanda Framework

How to work on the framework **itself** — the Go module and the embedded admin
UI. This is the deep technical guide; the contribution workflow (setup, PR
process, expectations) lives in `CONTRIBUTING.md`. For how to *use* Orjanda to
build an application, see `docs/getting-started.md`.

## Repository layout

The framework is a modular monolith with a strictly layered package DAG
(TAD §5.1). Top layers depend on bottom layers; circular imports are a design
error:

```
cmd/orjanda        Main
server             HTTP assembly, UI embedding
agent              Agent Runtime, LLM interactions
api                REST/RPC/WS handlers, middleware
document           Document Engine, validation
workflow perm audit event     business-logic services
dal                query generation, dialect drivers
schema             Registry, meta types
auth app           identity, JWT; application module system
errors config      primitives
```

`cache`, `search`, and `background` sit alongside `dal`. New packages belong
**inside** this layout — a package that fits nowhere in the DAG should not exist
(yet). Check TAD §5.1 before creating one.

## Building and testing

Full suite (everything CI runs):

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
golangci-lint run
```

### Integration tests (PostgreSQL)

Docker + testcontainers, gated behind the `integration` build tag:

```bash
go test -tags integration -count=1 ./testing/...
```

The `orjanda/testing` harness (`NewTestSite`, `WithApps`, `WithDialect`,
`CreateUser`, `WithUser`, `SeedFixtures`, `MockLLM`, … — TAD §17) provisions a
fully-wired site with an in-memory SQLite database per test, or a real
PostgreSQL instance under the integration tag. New framework tests should use
this harness, not ad hoc fixtures.

### The test suite golden rule

A change that touches a Document's schema, permissions, or agent-tool surface
should be checked against the worked examples in PRD §36–§38 — those scenarios
are the project's acceptance tests.

## Formatting and linting

```bash
gofmt -l .          # must report nothing
golangci-lint run   # errcheck, govet, staticcheck, gocritic, misspell, revive, noctx, bodyclose, unused, ineffassign
```

The golangci v2 config in `.golangci.yml` enables the `gofmt` formatter; CI
also runs an explicit `gofmt -l .` gate. Note the v2 formatter only checks
changed files by default — run `gofmt -l .` yourself to catch pre-existing
drift.

## Golden rules (binding on all code — TAD §1)

1. **Errors** — every fallible exported function returns/wraps `errors.Error`
   with one of the six `ErrorCode`s. No package invents a parallel error type.
2. **Context** — `auth.Identity`, `tenant_id`, `request_id` travel only via
   `context.Context` (typed, unexported keys). Never globals or extra params.
3. **Naming** — `oj` tag namespace; DocType → snake_case plural table
   (`LeaveRequest` → `leave_requests`); field → snake_case column.
4. **One permission path** — every read/write (REST, RPC, agent tool, workflow
   transition) goes through `perm.Engine`. A hand-rolled check outside it is a
   bug (PRD §25.1).
5. **Transactional audit** — every Document/Workflow write and its
   `audit.Entry` share one `dal.Tx`; a failed audit write rolls back the data
   write (TAD §13.1).
6. **Secure by default** — new capabilities ship permission-checked and
   audit-logged in the same change that exposes them.

## The embedded admin UI

`orjanda-ui/` is a React 19 + Vite + Tailwind app. The production build lives
in `orjanda-ui/dist/` and is **committed** — the Go binary embeds it via
`embed.FS` (`ui_embed.go`), so `go build ./...` never depends on a frontend
deploy step.

```bash
cd orjanda-ui
npm ci
npm run dev          # Vite dev server with HMR (points at a running `orjanda serve`)
npm run typecheck    # tsc --noEmit
npm test             # vitest
npm run build        # tsc --noEmit && vite build → overwrites dist/
```

### TypeScript SDK generation

The UI consumes a generated SDK (`orjanda-ui/src/generated/` — `schema.json`,
`types.ts`, `documents.ts`) derived from the compiled Registry. Regenerate it
whenever a Document schema changes:

```bash
npm run codegen      # runs node ../orjanda-codegen.mjs
```

Development `orjanda serve` runs a codegen pass on startup and regenerates the
output when the Registry's content hash changes; production
(`ORJANDA_ENV=production orjanda serve`) instead fails fast when the committed
`src/generated/schema.json` is stale (`ui/codegen.go`, TAD §16). If you change a
Document's fields, run codegen and commit the regenerated output **and** the
rebuilt `dist/` in the same PR — a stale `dist` breaks the Go embed.

## Writing a new dialect

Dialects live in `dal/dialect/<name>/` and must implement the full
`dal.Dialect` interface (query generation, placeholders, migrations,
`FullTextSearch`, …). PostgreSQL (`dal/dialect/postgres/`, pgx) and SQLite
(`dal/dialect/sqlite/`, modernc.org/sqlite, no CGo) are the reference
implementations. MySQL is explicitly post-MVP (PRD §44.3) — don't start one
without a scope change.

## Writing an extension point

The eleven extension surfaces are fixed (TAD §9). To implement one: pick the
interface, register it at its documented hook, and verify the default is
swappable by config, not by editing the Document Engine:

| Extension point | Interface | Register via |
|---|---|---|
| Document hooks | `event.Handler` | `event.Bus.On` |
| Custom API methods | `api.MethodHandler` | `api.RegisterMethod` |
| Permission rules | `perm.Rule` | `perm.Engine.RegisterRule` |
| Field validators | `schema.Validator` | `oj:"validator=Name"` + `schema.RegisterValidator` |
| Auth providers | `auth.Provider` | `site.Config.Auth.Provider` |
| LLM providers | `llm.Provider` | `site.Config.LLM.Providers[name]` |
| Agent tools | `agent.Tool` | `agent.Runtime.RegisterTool` |
| Search backends | `search.Backend` | `site.Config.Search.Backend` |
| Cache backends | `cache.Store` | `site.Config.Cache.Store` |
| Background jobs | `background.Job` | `background.Queue.RegisterHandler` |
| UI pages | `ui.Page` | `ui.Registry.RegisterPage` |

Respect the `O(len(CompiledDocs))` tool-count guarantee when extending the
agent tool generator (TAD §10.1) and keep the discovery-vs-operation tool split
(TAD §11.1).

## Documenting changes

- Cite PRD/TAD section numbers in comments and commits instead of restating
  rationale (e.g. `// see TAD §8.1 step 3`).
- Update `CHANGELOG.md` under `[Unreleased]` for user-visible changes.

## History notes

The internal `docs/ORJANDA-IMPLEMENTATION-PLAN.md` was deleted as part of the
open-source cleanup (it referenced internal review findings). A sanitized,
public-facing roadmap may be restored later; until then, the phased plan is not
public documentation — do not re-create it without a scope decision.
