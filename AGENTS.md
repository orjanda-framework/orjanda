# AGENTS.md

Guide for AI coding agents working in this repository. Read this before touching code.
It is a navigation aid, not a spec — the specs live in `docs/` and always win on detail.

## 0. Repository State (read this first)

This repository contains the **fully implemented Orjanda framework**: the Go module
(module `github.com/orjanda-framework/orjanda`, `go 1.26.5`) is on disk with all packages
in the layout described in §4, and `orjanda-ui/` ships the React admin UI with a committed
`dist/`. Before assuming a file or package exists, check the tree with `ls`/`find` — but
unlike earlier revisions, the structure below is the actual layout, not a target.

This is an **open-source project**. The public-facing documents (`README.md`,
`CONTRIBUTING.md`, `SECURITY.md`, `SUPPORT.md`, `GOVERNANCE.md`, `CODE_OF_CONDUCT.md`,
`CHANGELOG.md`, and the `docs/*.md` guides) are the release artifacts; PRD/TAD remain the
engineering source of truth. Do not create new internal-only documents without a scope
decision — the internal implementation plan was deleted as part of the cleanup (see
`OPENSOURCE-RELEASE-PLAN.md` if present, and `docs/development.md` "History notes").

---

## 1. What Orjanda Is

Orjanda is an open-source, **agent-native business application framework written in Go**.
A developer defines a business entity once (a `Document`, as a Go struct with `oj` struct
tags), and the framework derives — automatically, from that single declaration — a database
table, a REST/RPC API, an admin UI, and a set of AI-agent tools, all enforcing the same
permission rules. The framework's central thesis: *if a Document exists in the Registry,
it is automatically operable by both a human and an embedded AI agent, with no per-entity
integration code.*

It ships as a **single Go binary** (modular monolith, not microservices — see PRD §9.1),
with an embedded React admin UI and an embedded AI Agent Runtime that calls the exact same
`document.Create/Read/Update/Delete` functions the REST API calls — there is no separate
"agent backend" and no separate agent permission path (PRD §23.1, §25.1).

Full product rationale: `docs/ORJANDA-PRD.md` §1–§9.
Full architecture/interfaces: `docs/ORJANDA-TAD.md` §1–§2.

---

## 2. The Documentation Set (read in this order)

| File | Answers | Use it to... |
|---|---|---|
| `docs/ORJANDA-PRD.md` | *Why* and *what* — product goals, decisions, rationale, examples | Understand intent before changing behavior |
| `docs/ORJANDA-TAD.md` | *Exact shape* — Go interfaces, struct fields, algorithms, data flows | Get the literal type signature / contract before writing code |
| `docs/getting-started.md` | How to use Orjanda to build an application | Follow the end-user onboarding flow |
| `docs/architecture.md` | Prose-level tour of the pipeline | Orient before reading the TAD |
| `docs/configuration.md` | User-facing config reference | Check a `orjanda.yaml` key or `ORJANDA_*` env var |
| `docs/development.md` | How to develop the framework itself | Deep technical guide (deliberately disjoint from CONTRIBUTING.md) |

**Rule for agents: never invent an interface, package name, or field the TAD doesn't
already specify.** If the TAD is silent on something you need, say so explicitly rather than
improvising new architecture — this project's docs are the source of truth by design.

This file does not repeat PRD/TAD content — it tells you *where* to find it.

---

## 3. Core Concepts (one line each — full definitions in PRD §7)

- **Document** — a declared business entity (Go struct + `oj` tags + `DocMeta()`).
- **Schema** — a Document's field/constraint/relationship declaration; the single source of truth for DB, API, UI, and agent tools.
- **Application** — a distributable Go module bundling related Documents, hooks, workflows (PRD §11).
- **Module** — a logical grouping of Documents inside an Application (no runtime behavior of its own).
- **Registry** — the compiled, read-only, in-memory catalog of every Document, built once at startup (PRD §10.5, TAD §3.1).
- **Agent Runtime** — the embedded subsystem that turns the Registry into per-identity LLM tool definitions and executes them through the same Document Engine as everything else.
- **Hook** — a lifecycle callback (`before_save`, `after_insert`, ...) — PRD §19.1.
- **Workflow** — a declared state machine governing a Document's transitions — PRD §19.3 / TAD §8.

---

## 4. Project Structure

### 4.1 Current (actual, on disk today)
```
.
├── AGENTS.md                          # this file
├── README.md                          # public-facing landing page
├── CONTRIBUTING.md, SECURITY.md, SUPPORT.md,
│   GOVERNANCE.md, CODE_OF_CONDUCT.md, CHANGELOG.md,
│   LICENSE                            # release artifacts (Apache-2.0)
├── .github/                           # CI + issue/PR templates
├── orjanda.yaml                       # reference config (env-interpolated)
├── orjanda-codegen.mjs                # UI codegen script
├── ui_embed.go, site.go               # composition root + embedded UI
├── cmd/orjanda/                       # CLI binary (Cobra)               — TAD §16
├── errors/                            # ErrorCode, Error interface       — TAD §1.1
├── config/                            # Viper-backed orjanda.yaml/env    — TAD §1.3
├── app/                               # app.Definition, dependency DAG   — TAD §7
├── schema/                            # Document, Registry, CompiledDoc  — TAD §2.1-2.2
├── dal/                               # Database, Dialect, Migrator      — TAD §2.3, §14
│   ├── dialect/postgres/              # PostgreSQL adapter (pgx)
│   └── dialect/sqlite/                # SQLite adapter (modernc.org/sqlite)
├── cache/                             # cache.Store (in-process LRU)     — TAD §9.1
├── search/                            # search.Backend (dialect FTS)     — TAD §9.1
├── background/                        # background.Job/Queue (stub)      — TAD §9.1
├── document/                          # Document Engine: CRUD, validation — TAD §3.2
├── event/                             # event.Bus, lifecycle hooks       — TAD §2.5
├── perm/                              # perm.Engine (RBAC+ABAC)          — TAD §2.4/§2.7
├── workflow/                          # workflow.Engine, state machine   — TAD §8
├── audit/                             # audit.Log, transactional writes  — TAD §13
├── auth/                              # Identity, auth.Provider, JWT     — TAD §9.1
├── orjanda-core/                      # bootstrapped User/Role app       — TAD §4
├── api/                               # REST + RPC + Metadata API        — PRD §14
│   ├── rest/  ├── rpc/  └── middleware/
├── agent/                             # Agent Runtime
│   ├── llm/                            # llm.Provider (OpenAI, Anthropic) — TAD §2.7
│   ├── tools/                          # ToolRegistry: Compile/ForIdentity — TAD §10
│   ├── runtime/                        # Execute loop, Session Manager    — TAD §11
│   ├── planner/                        # ReAct + Plan-and-Execute         — TAD §11.2-11.3
│   └── safety/                         # SafetyLayer, approvals           — TAD §12
├── ui/                               # ui.Page registry, metadata        — TAD §9.1/§6.1
├── server/                           # Site composition root, HTTP       — TAD §12.4
├── testing/                          # orjanda/testing harness           — TAD §17
├── cli/                              # command impls backing cmd/orjanda — TAD §16
├── docs/                             # PRD, TAD, and the docs/* guides
└── orjanda-ui/                       # React + Tailwind Admin UI (embedded via embed.FS) — PRD §17
```
If you are about to create a package not listed above, stop and check the TAD — it is
almost certainly meant to live inside one of these, not beside them. (`orjanda-app-hr-example`
was removed from the repo; the HR scenarios in PRD §36–§38 remain the acceptance tests.)

---

## 5. Where to Look, by Task

| If you're asked to... | Read first | Implement in | Sequencing / prerequisites |
|---|---|---|---|
| Add a new `oj` field annotation | PRD §10.4, TAD §2.1 | `schema/` | Registry compiler must parse it before anything downstream sees it |
| Change how tables/columns are named | TAD §1.4 | `dal/dialect/*` | Naming rules are fixed — don't invent new casing |
| Add a database dialect | PRD §13.3, TAD §2.3 | `dal/dialect/<name>/` | Implement the full `Dialect` interface; MySQL is explicitly post-MVP (PRD §44.3) |
| Change migration behavior | TAD §14 | `dal/migrate` (`Migrator`) | Respect the `--allow-destructive` gate; never auto-generate `Down` SQL |
| Add/modify a lifecycle hook point | PRD §19.1, TAD §2.5 | `event/` | Must integrate through `document/`'s existing dispatch, not a new path |
| Add a permission rule type | PRD §16, TAD §2.4/§2.7/§9.1 (`perm.Rule`) | `perm/` | RBAC check always runs first; Rules are AND-composed after |
| Change workflow transition logic | PRD §19.3, TAD §8 | `workflow/` | Role checks MUST go through `perm.Engine` — no bespoke check |
| Add an extension point (new interface) | TAD §9 (table of all 11 extension points) | matching package (`search/`, `cache/`, `background/`, `ui/`, etc.) | Check TAD §9.3's resolution table before adding a 12th kind |
| Modify agent tool generation | PRD §24, TAD §10 | `agent/tools/` | Preserve the `O(len(CompiledDocs))` tool-count guarantee (TAD §10.1 step 7) |
| Change agent context/planning behavior | PRD §23.4/§27, TAD §11 | `agent/runtime/`, `agent/planner/` | Discovery vs. operation tool split (TAD §11.1) must be preserved |
| Change approval/safety policy | PRD §28, TAD §12 | `agent/safety/` | `AlwaysRequireApproval` and bulk-limit checks are non-configurable — see TAD §12.1 |
| Add a REST/RPC endpoint | PRD §14, TAD §3.2 | `api/rest/` or `api/rpc/` | Must go through the same middleware chain: CORS → Auth → RateLimit → Permission |
| Add a CLI command | PRD §21, TAD §16 | `cli/` + `cmd/orjanda` | Follow the `serve` (dev, forgiving) vs. `bench` (prod, fail-fast) distinction |
| Change the Admin UI rendering | PRD §17-18 | `orjanda-ui/src/` | Forms/lists must stay metadata-driven — no hardcoded per-Document UI |
| Add a test utility | PRD §32, TAD §17 | `testing/` | Extend `orjanda/testing`, don't hand-roll a parallel fixture system |
| Anything involving multi-tenancy | PRD §30, TAD §15 | `dal/` (`TenantScopedDB`), `auth/` | Stays wired but **inactive** (`MultiTenant: false`) — see §7 below |

---

## 6. Key Conventions (binding on all code — TAD §1, Implementation Plan §3)

1. **Errors:** every fallible exported function returns/wraps `errors.Error` with one of the six `ErrorCode`s (TAD §1.1). No package invents its own error type for a condition that enum already covers.
2. **Context:** `auth.Identity`, `tenant_id`, `request_id` travel only via `context.Context` (typed, unexported keys). Never through globals or extra params.
3. **Naming:** `oj` struct tag namespace; DocType → snake_case plural table (`LeaveRequest` → `leave_requests`); field → snake_case column.
4. **One permission path:** every read/write — REST, RPC, agent tool, workflow transition — calls through `perm.Engine`. A hand-rolled check outside it is a bug, not a shortcut (PRD §25.1).
5. **Transactional audit:** every Document/Workflow Engine write and its `audit.Entry` share one `dal.Tx`; a failed audit write rolls back the data write (TAD §13.1).
6. **Secure by default:** new capabilities ship permission-checked and audit-logged in the same change that exposes them — never as a follow-up.
7. **Code-first schemas only:** Documents are Go structs compiled at startup, never runtime-editable JSON/YAML metadata (PRD §8.4). Don't add a schema-editing UI or API.
8. **No parallel agent execution path:** the Agent Executor must call the same `document.*`/`workflow.*` functions the API layer calls (PRD §23.1). If you're writing agent-only business logic that bypasses the Document Engine, stop.

---

## 7. Boundaries — Do Not (PRD §5 Non-Goals, §44.3 MVP Non-Scope, Plan §6)

- Do not introduce microservices, gRPC-as-primary-API, or GraphQL — REST + RPC only (PRD §14.1).
- Do not build runtime/hot-swappable plugin loading — Applications are compile-time Go imports only (PRD §20.1).
- Do not activate multi-tenancy, add a MySQL dialect, wire real OpenTelemetry, or make background jobs durable — these are stubbed/inert by design until v0.2 (PRD §44.3).
- Do not build an MCP server interface, a visual workflow designer, or cross-app schema extension — all v0.3/v0.4 (PRD §43.1).
- Do not make the agent fully autonomous / remove approval gates — the Safety Layer (TAD §12) is mandatory, not optional configuration (PRD NG3).
- Do not build a specific business product (ERP, CRM, etc.) inside the framework itself — `orjanda-app-hr-example` is a reference/validation Application only (PRD NG1).
- Do not restate PRD/TAD rationale in new docs or code comments at length — cite the section (e.g. `// see TAD §8.1 step 3`) instead of re-explaining it.

If a request conflicts with something in this section, flag the conflict rather than
quietly implementing it — these boundaries are deliberate scope limits, not oversights.

---

## 8. Technology Stack (PRD §42 — do not substitute without updating the TAD first)

| Concern | Choice |
|---|---|
| Language | Go 1.26+ |
| HTTP router | Chi |
| CLI | Cobra |
| DB drivers | `pgx` (PostgreSQL), `modernc.org/sqlite` (SQLite, no CGo) |
| Migrations | Atlas (diffing) + Goose (execution) |
| Auth | `golang-jwt/jwt`, `bcrypt` |
| IDs | ULID (`oklog/ulid`) |
| Logging | `log/slog` (stdlib, structured — TAD §13.3 fixed key set) |
| Config | Viper |
| Testing | `testify`, `testcontainers-go` |
| Frontend | React 19+, Vite, Tailwind CSS |
| UI embedding | `embed.FS` (stdlib) — single-binary deploy |
| Decimal math | `shopspring/decimal` |

---

## 9. Build, Test, and Verify

The framework is implemented, so the CI commands below run and must pass. CI
(`.github/workflows/ci.yml`) mirrors them exactly: `go build ./...`, `go vet`,
`go test -race -count=1 ./...`, golangci-lint (with the `gofmt` formatter and
an explicit `gofmt -l .` gate), `go mod tidy` drift check, the testcontainers
integration lane (`go test -tags integration -count=1 ./testing/...`), and the
frontend lane (`npm ci && npm run typecheck && npm test && npm run build` in
`orjanda-ui/`).

---

## 10. Workflow Checklist for Any Change

1. **Locate intent** — find the relevant PRD section (why does this need to exist / what should it do).
2. **Locate contract** — find the relevant TAD section (exact interface, types, algorithm). If it's not there, don't invent one — ask.
3. **Locate sequencing** — find the relevant documentation (this file, `docs/development.md`) or the open-source release plan for what must already exist.
4. **Implement** inside the package the tree in §4.1 assigns it to.
5. **Verify** with the build/test/lint suite in §9 before considering the change finished.
6. **Cite, don't copy** — reference PRD/TAD section numbers in comments and commit messages instead of duplicating their text.