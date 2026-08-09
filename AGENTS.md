# AGENTS.md

Guide for AI coding agents working in this repository. Read this before touching code.
It is a navigation aid, not a spec — the specs live in `docs/` and always win on detail.

## 0. Repository State (read this first)

This repository is currently **documentation-only, pre-implementation**. `docs/` contains
the full specification; no Go/TypeScript source exists yet. Everything in this file that
describes "where X lives" is describing the **target layout defined by the docs**, not code
that is already on disk. Before assuming a file or package exists, check the tree with `ls`/`find`
— do not assume the planned structure below is already populated.

Work should follow `docs/ORJANDA-IMPLEMENTATION-PLAN.md`'s phase order (Phase 0 → Phase 12).
If asked to "add feature X," first locate which phase X belongs to and confirm its declared
dependencies (earlier phases) are actually present before building on top of them.

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
| `docs/ORJANDA-IMPLEMENTATION-PLAN.md` | *Order and done-ness* — phases, dependencies, deliverables, completion checklists | Know what phase you're in, what must already exist, and how to tell you're finished |

**Rule for agents: never invent an interface, package name, or field the TAD doesn't
already specify.** If the TAD is silent on something you need, say so explicitly rather than
improvising new architecture — this project's docs are the source of truth by design
(Implementation Plan §1: "introduces no new architecture, interfaces, or scope").

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
└── docs/
    ├── ORJANDA-PRD.md
    ├── ORJANDA-TAD.md
    └── ORJANDA-IMPLEMENTATION-PLAN.md
```

### 4.2 Target (to be created per the Implementation Plan phases — package names and
purposes are fixed by PRD §12.1 and TAD §5.1; do not rename or restructure them)
```
orjanda/                      # framework module (github.com/orjanda-framework/orjanda)
├── cmd/orjanda/               # CLI binary (Cobra)                         — TAD §16, Plan Phase 10
├── errors/                    # ErrorCode, Error interface                 — TAD §1.1, Plan Phase 0
├── config/                    # Viper-backed orjanda.yaml / env loader     — TAD §1.3, Plan Phase 0
├── app/                       # app.Definition, dependency DAG, lifecycle  — TAD §7,   Plan Phase 1
├── schema/                    # Document, Registry, CompiledDoc, oj-tags   — TAD §2.1-2.2, Plan Phase 1
├── dal/                       # Database, Dialect, query builder, Migrator — TAD §2.3, §14, Plan Phase 2
│   ├── dialect/postgres/      # PostgreSQL adapter (pgx)
│   └── dialect/sqlite/        # SQLite adapter (modernc.org/sqlite)
├── cache/                     # cache.Store (in-process LRU default)      — TAD §9.1, Plan Phase 2
├── search/                    # search.Backend (dialect FTS default)      — TAD §9.1, Plan Phase 2
├── background/                # background.Job/Queue (inert stub in MVP) — TAD §9.1, Plan Phase 2
├── document/                  # Document Engine: CRUD, validation         — TAD §3.2, Plan Phase 3-4
├── event/                     # event.Bus, lifecycle hooks                — TAD §2.5, Plan Phase 4
├── perm/                      # perm.Engine (RBAC+ABAC), perm.Rule        — TAD §2.4/§2.7, Plan Phase 4
├── workflow/                  # workflow.Engine, state machine            — TAD §8,   Plan Phase 4
├── audit/                     # audit.Log, Entry, transactional writes    — TAD §13,  Plan Phase 4
├── auth/                      # Identity, auth.Provider, JWT default      — TAD §9.1, Plan Phase 5
├── orjanda-core/               # bootstrapped User/Role/RolePermission app — TAD §4,   Plan Phase 5
├── api/                       # REST + RPC + Metadata API, middleware     — PRD §14, TAD §3.2, Plan Phase 6
│   ├── rest/  ├── rpc/  └── middleware/
├── agent/                     # Agent Runtime
│   ├── llm/                    # llm.Provider (OpenAI, Anthropic)         — TAD §2.7, Plan Phase 7
│   ├── tools/                   # ToolRegistry: Compile/ForIdentity       — TAD §10,  Plan Phase 7
│   ├── runtime/                  # Execute loop, Session/Context Manager  — TAD §11,  Plan Phase 8
│   ├── planner/                   # ReAct + Plan-and-Execute, Plan schema — TAD §11.2-11.3, Plan Phase 8
│   └── safety/                     # SafetyLayer, SafetyPolicy, approvals — TAD §12,  Plan Phase 8
├── ui/                         # ui.Page registry, metadata serving       — TAD §9.1/§6.1, Plan Phase 9
├── server/                     # Site composition root, HTTP assembly    — TAD §12.4, Plan Phase 6/9
├── testing/                    # orjanda/testing harness (NewTestSite...) — TAD §17,  Plan Phase 11
└── cli/                        # command implementations backing cmd/orjanda — TAD §16, Plan Phase 10

orjanda-ui/                    # React + Tailwind Admin UI (embedded via embed.FS) — PRD §17, Plan Phase 9
orjanda-app-hr-example/        # reference Application used for MVP validation — PRD §36, Plan Phase 12
```
If you are about to create a package not listed above, stop and check the TAD — it is
almost certainly meant to live inside one of these, not beside them.

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
| Language | Go 1.22+ |
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

No build/test commands exist yet — they come online starting **Plan Phase 0** (CI: `go build
./...`, `go vet`, `golangci-lint`, unit tests) and **Plan Phase 10** (the `orjanda` CLI itself,
including `orjanda test`). Once code exists:

- Check `docs/ORJANDA-IMPLEMENTATION-PLAN.md`'s per-phase **Completion criteria** checklist
  for the phase you're working in — that checklist is the acceptance bar, not "it compiles."
- Prefer writing tests via `orjanda/testing` (Phase 11) over ad hoc fixtures once that
  package exists; before it exists, follow the harness shape already specified in TAD §17
  so later retrofitting is mechanical.
- A change that touches a Document's schema, permissions, or agent-tool surface should be
  checked against the worked examples in PRD §36–§38 (the HR reference scenarios) — those
  are the plan's actual acceptance tests (Plan Phase 12).

---

## 10. Workflow Checklist for Any Change

1. **Locate intent** — find the relevant PRD section (why does this need to exist / what should it do).
2. **Locate contract** — find the relevant TAD section (exact interface, types, algorithm). If it's not there, don't invent one — ask.
3. **Locate sequencing** — find the relevant Implementation Plan phase (what must already exist; what "done" means for this change).
4. **Implement** inside the package the tree in §4.2 assigns it to.
5. **Verify** against that phase's completion criteria before considering the change finished.
6. **Cite, don't copy** — reference PRD/TAD section numbers in comments and commit messages instead of duplicating their text.