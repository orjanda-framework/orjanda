# Orjanda — Implementation Plan

**Version:** 1.0.0
**Date:** 2026-08-09
**Status:** Engineering Specification
**Sources:** `docs/ORJANDA-PRD.md` (v1.0.0), `docs/ORJANDA-TAD.md` (v1.1.0)

---

## 1. Purpose and Scope

This document sequences the implementation of Orjanda into dependency-ordered phases with concrete deliverables, entry/exit criteria, and completion checks. It introduces **no new architecture, interfaces, or scope**. Every deliverable below traces to a specific section of the PRD and/or TAD; where this plan adds detail (e.g. splitting a TAD milestone into engineer-sized tasks), that detail is a re-sequencing of existing decisions, not a new one.

This plan covers the **MVP** as defined in PRD §44. Post-MVP items (PRD §43.1, §44.3) are listed in §8 for orientation only and are explicitly out of scope for the phases below.

### 1.1 How to Read This Plan
Each phase has five parts:
- **Objective** — what capability exists once the phase is done.
- **Source references** — the PRD/TAD sections that define the work (consult these for the authoritative design; this plan does not restate interface bodies already fully specified there).
- **Deliverables** — concrete artifacts (packages, types, endpoints, CLI commands) an engineer produces.
- **Dependencies** — which phases must be complete first, and why.
- **Completion criteria** — testable conditions that gate moving to the next phase.

Phases are numbered in build order, not priority order — a later phase is later because something in an earlier phase is a hard technical prerequisite, per the package dependency graph in TAD §5.1.

---

## 2. Build Order Rationale

TAD §5.1 defines a strict layered package DAG (higher layers depend on lower ones) and §5.2 defines the `NewSite()` initialization order. This plan's phase order is that DAG read bottom-up — each phase implements one or more DAG layers, starting from the layer with no internal dependencies:

| Phase | TAD DAG layer(s) implemented | Packages |
|---|---|---|
| 0 | 10 (primitives) | `errors`, `config` |
| 1 | 9, 8 (+ `app`) | `auth` (types only), `schema`, `app` |
| 2 | 7 | `dal` (query builder, dialects, migrator), `cache`, `search` (stub) |
| 3 | 5 (partial) | `document` (bare CRUD) |
| 4 | 6 | `event`, `perm`, `workflow`, `audit`, `background` (stub) — integrated into `document` |
| 5 | 9 (impl) | `auth` (JWT provider), `orjanda-core` |
| 6 | 4 | `api` (REST, RPC, middleware) |
| 7 | 3 (part 1) | `agent/llm`, `agent/tools` |
| 8 | 3 (part 2) | `agent/runtime`, `agent/planner`, `agent/safety` |
| 9 | 2 (frontend half) | `ui`, `orjanda-ui` (React), `@orjanda/codegen` |
| 10 | 1 | `cli`, `cmd/orjanda` |
| 11 | cross-cutting | `orjanda/testing` |
| 12 | cross-cutting | remaining extension stubs, HR example app, MVP sign-off |

This ordering also matches TAD §18's M1–M7 milestones; this plan simply expands each milestone into engineer-actionable phases and makes the entry/exit criteria explicit.

---

## 3. Cross-Cutting Engineering Standards

The following constraints apply to **every** phase below and are not repeated per phase. They come from TAD §1 and PRD §8.6 / §25.1.

1. **Error Model** (TAD §1.1): every exported function that can fail returns an `errors.Error` (or wraps one via `Unwrap()`). No package invents its own error type for conditions the `ErrorCode` enum already covers.
2. **Context Propagation** (TAD §1.2): `auth.Identity`, `tenant_id`, and `request_id` are threaded through `context.Context` only — never through global state, package-level variables, or additional function parameters.
3. **Naming Conventions** (TAD §1.4): `oj` struct tags; DocType → snake_case plural table names; struct field → snake_case column names. Enforced by a lint check introduced in Phase 1.
4. **No Parallel Permission Paths** (PRD §25.1, §16.3): any code path that reads or writes a Document — REST handler, RPC method, agent tool, workflow transition — must call through `perm.Engine`. A permission check hand-rolled outside `perm.Engine` is a defect, not a shortcut.
5. **Transactional Audit Writes** (TAD §13.1): every Document Engine or Workflow Engine write is wrapped in a `dal.Tx` that includes the audit write; a failed audit write rolls back the data write.
6. **Secure Defaults** (PRD §8.6): new capabilities ship permission-checked and audit-logged from the first commit that exposes them externally — enforcement is never a follow-up task.

---

## 4. Phase Plan

### Phase 0 — Repository Scaffolding & Foundational Primitives ✅ COMPLETE

**Objective:** A compilable, empty Orjanda module with the primitives every other package depends on.

**Source references:** PRD §12.1 (package layout), §42 (technology choices); TAD §1.1 (`errors`), §1.3 (`config`), §5.1 (DAG layer 10).

**Deliverables:**
- Go module scaffold matching the `orjanda/` package layout in PRD §12.1 (empty package directories with `doc.go` stubs for every package named in the layout, so the DAG is visible in the repo from day one).
- `errors` package: `ErrorCode` enum and `Error` interface exactly as specified in TAD §1.1, with constructor helpers (`errors.Validation(...)`, `errors.Permission(...)`, etc.) and an HTTP status mapping table.
- `config` package: Viper-backed loader implementing the `orjanda.yaml` / `ORJANDA_`-prefixed env schema in TAD §1.3, with struct-tag-bound config types for `server`, `database`, `llm`, `llm.safety`.
- CI pipeline: `go build ./...`, `go vet`, `golangci-lint` (including the naming-convention lint from §3 item 3), unit test runner.

**Dependencies:** None — this is the root of the build.

**Completion criteria:**
- [x] `go build ./...` succeeds with all package stubs in place.
- [x] `errors.Error` round-trips through the HTTP status mapping table for all six `ErrorCode` values.
- [x] `config.Load()` correctly parses the example `orjanda.yaml` from TAD §1.3, with env var override verified for at least one nested key (`ORJANDA_OPENAI_API_KEY`).
- [x] CI pipeline is green on an empty commit.

---

### Phase 1 — Schema, Registry & Application/Module System ✅ COMPLETE

**Objective:** A developer can define a `Document` struct with `oj` tags and a `DocMeta()`, register it via an `app.Definition`, and have it compile into a queryable, read-only `Registry`.

**Source references:** PRD §7.1–§7.5, §8.4, §10 (all), §11 (all); TAD §2.1–§2.2, §3.1 (Registry Compilation Pipeline), §7 (Application & Module System Contract).

**Deliverables:**
- `schema` package: `Document` interface, `BaseDocument`/`BaseChild` embeds (auto fields per PRD §10.2), `Meta`/`CompiledDoc`/`Field`/`DocPermission` types, full `oj` tag parser covering every annotation in PRD §10.4, and `schema.Validator` interface + `RegisterValidator` (TAD §9.1).
- `app` package: `Definition`, `Module`, `Dependency` types; `Installable`/`Upgradable`/`Uninstallable` optional interfaces; dependency-DAG resolution and hook-conflict ordering exactly as specified in TAD §7.1.
- `schema.Registry` implementation: `Register`, `Compile` (six-step pipeline per PRD §10.5 / TAD §3.1), `Get`, `List`, `Relationships`. Compile enforces: Link target existence, circular child-table rejection, and (per TAD §7.1 step 2) app-dependency-ordered registration.
- Reference dummy Application (throwaway, test-only) exercising every `oj` tag and both parent/child Documents, used purely to validate the compiler.

**Dependencies:** Phase 0 (`errors`, `config`).

**Completion criteria:**
- [x] Defining a Document with every `oj` annotation from PRD §10.4 produces a correct `CompiledDoc` (field-by-field assertion test).
- [x] A Link to a non-existent DocType fails `Compile()` with `errors.CodeValidation`.
- [x] A circular child-table definition fails `Compile()`.
- [x] Two Applications with a `Dependency` between them register hooks in dependency order, not declaration order (TAD §7.1 step 3, directly tested).
- [x] `Registry.Compile()` on 100 dummy Documents completes in < 2s (PRD §33.1 target, tested here since it's the first point the target is measurable).
- [x] Registry is provably immutable after `Compile()` (attempting `Register()` post-compile returns an error).

---

### Phase 2 — Data Access Layer, Dialects & Migrations ✅ COMPLETE

**Objective:** Registry-driven SQL generation against both MVP dialects, plus a working diff/apply migration pipeline.

**Source references:** PRD §13 (all); TAD §2.3 (`dal.Database`/`Dialect`), §9.1 (`cache.Store`, `search.Backend`), §14 (Migration Pipeline Contract).

**Deliverables:**
- `dal` package: `Database`, `Tx`, `Dialect`, `Select` types per TAD §2.3; query builder implementing field resolution, permission-filter injection points (hooks only — actual filtering lands in Phase 4), Link-traversal JOINs, and delegation to `Dialect`.
- PostgreSQL dialect (`pgx` driver) and SQLite dialect (`modernc.org/sqlite`), both implementing `CreateTable`, `AlterTable`, `SelectSQL`, `InsertSQL`, `UpdateSQL`, `DeleteSQL`, `FullTextSearch`, `Placeholder`.
- `dal.Migrator`: `Diff` (Atlas-backed), `Write` (Goose-formatted files, `--allow-destructive` gate per TAD §14.1 step 2), `Up`, `Status`; multi-dialect file generation.
- `cache` package: `Store` interface + in-process LRU default implementation (TAD §9.1).
- `search` package: `Backend` interface + default adapter over `Dialect.FullTextSearch` (TAD §9.1) — no external search process in MVP.
- `background` package: `Job`/`Queue` interfaces + non-durable in-memory `Queue` stub (TAD §9.1) — present for later Application authors, inactive/no-op beyond accepting registrations in MVP.

**Dependencies:** Phase 1 (`schema.Registry`, `CompiledDoc` are the direct inputs to `Diff` and `CreateTable`).

**Completion criteria:**
- [x] `Diff` against an empty database for the Phase 1 reference Application produces `CreateTable` statements for both dialects.
- [x] `migrate up` applied twice is idempotent (no error, no duplicate DDL) on both dialects.
- [x] A destructive change (dropped column) is excluded from `Write`'s output unless `--allow-destructive` is set.
- [x] Snapshot tests confirm identical logical SQL semantics between the PostgreSQL and SQLite dialects for the same `Select` (PRD §40 Risk R2 mitigation).
- [x] `FullTextSearch` returns matching IDs for a `Searchable` field on both dialects.
- [x] `cache.Store` `Get`/`Set`/`Delete` round-trip correctly with TTL expiry.

---

### Phase 3 — Document Engine (Bare CRUD) ✅ COMPLETE

**Objective:** Create/Read/Update/Delete/List against the DAL through the Document Engine, with schema validation but **without** permissions, hooks, or workflow (those arrive in Phase 4) — matching TAD §18 M2's explicit scoping.

**Source references:** PRD §12.2 (Request Lifecycle, validation portion); TAD §3.2 (steps limited to validation + DAL operation), §5.1 DAG layer 5.

**Deliverables:**
- `document` package: `Create`, `Read`, `Update`, `Delete`, `List` operating on `map[string]any` payloads (per TAD §2.3's DAL shape), driving field validation (`required`, `unique`, `format`, `options`, custom `schema.Validator`s) from `CompiledDoc`.
- Soft-delete handling (`Deleted` flag, PRD §10.2) and `DocStatus` handling for `Submittable` Documents.
- Transaction wrapping for all writes (TAD §3.6/PRD §13.6) — no hook dispatch yet, but the `dal.Tx` boundary is established here since Phase 4 threads hook calls inside it.

**Dependencies:** Phase 2 (DAL, dialects, migrations must exist to persist anything).

**Completion criteria:**
- [x] Full CRUD round-trip against both dialects via `document.Create/Read/Update/Delete/List`, called directly (no HTTP layer yet).
- [x] A `required` field violation, a `unique` constraint violation, and a `format=email` violation each return `errors.CodeValidation` with field-level `Details()`.
- [x] Deleting a record sets `Deleted=true` and excludes it from `List` by default.
- [x] Every write operation is transactional: a forced failure mid-write (test-only fault injection) leaves the database unchanged.

---

### Phase 4 — Business Logic Services & Engine Integration ✅ COMPLETE

**Objective:** The Document Engine enforces permissions, fires lifecycle hooks, supports declared workflows, and writes an immutable audit trail — completing the request lifecycle in PRD §12.2 / TAD §3.2 in full.

**Source references:** PRD §16 (Permissions Model), §19 (Workflow/Event System), §29.1/§29.3 (Audit Log, structured logging); TAD §2.4 (`perm.Engine`) + §2.7 extension, §2.5 (`event.Bus`), §8 (Workflow Engine Contract), §9.1 (`perm.Rule`), §13 (Audit & Observability Contract).

**Deliverables:**
- `event` package: `Bus`, `Handler`, `On`/`Emit`, dispatching the full lifecycle event set from PRD §19.1 table, in app-dependency order (Phase 1) within an app, declaration order across.
- `perm` package: `Engine` per TAD §2.4 plus the §2.7 extensions (`AllowedFields`, `RegisterRule`); RBAC document-level checks from `DocPermission`; ABAC via `perm.Rule` (TAD §9.1); `FilterRead`/`FilterWrite` field-level filtering.
- `workflow` package: `Definition`, `State`, `Transition`, `Engine.Register/AvailableTransitions/Execute` exactly per TAD §8, including the auto-added `WorkflowState` field and the shared-`perm.Engine` role check (§8.1 step 3).
- `audit` package: `Entry`, `FieldChange`, `Log.Write/Query` per TAD §13, with the transactional write guarantee (§13.1) and pre/post-image diffing (§13.2) wired into the Document Engine and Workflow Engine.
- Document Engine updated to run the full pipeline: `before_validate` → validate → `after_validate` → `perm.FilterWrite` → `before_insert`/`before_save` → DAL write → `after_insert`/`after_save` → audit write → commit (TAD §3.2).
- `slog`-based structured logging per TAD §13.3's fixed key set, wired at every emission point named there.

**Dependencies:** Phase 3 (bare Document Engine to integrate into) and Phase 1 (`DocPermission` metadata, workflow-bearing `CompiledDoc`).

**Completion criteria:**
- [x] A `before_save` hook that returns an error aborts the transaction; no partial write occurs (PRD §19.2 semantics).
- [x] A user without `Create` permission on a DocType receives `errors.CodePermission` from `document.Create`, before any DAL call is made.
- [x] A field tagged `permission=role` is absent from `FilterRead`'s output for a caller lacking that role, and rejected (not silently dropped) from `FilterWrite`'s input.
- [x] A `workflow.Execute` call from a disallowed role returns `errors.CodePermission` via the same `perm.Engine` path used by `document.Create` (single code path, per §3 item 4 above).
- [x] Every successful Create/Update/Delete/workflow transition produces exactly one `audit.Entry` with a correct `Changes` diff, written in the same transaction (kill the process mid-write in a test harness; confirm no orphaned data change without a matching audit row).
- [x] `perm.Rule` composes with RBAC via AND semantics (a passing RBAC check can still be denied by a registered Rule).

---

### Phase 5 — Authentication & Core Application ✅ COMPLETE

**Objective:** Real users can authenticate, hold roles, and the system bootstraps a working administrator on first run.

**Source references:** PRD §15 (all); TAD §4 (Core Application Schema), §9.1 (`auth.Provider`).

**Deliverables:**
- `auth` package: `Identity`, `UserInfo`, `Provider` interface (TAD §9.1); built-in JWT provider — bcrypt password hashing, 15-minute access tokens, 7-day refresh tokens with rotation (PRD §15.1).
- `orjanda-core` Application: `User`, `UserRole`, `Role`, `RolePermission` Documents (TAD §4.1), registered via the Phase 1 `app.Definition` mechanism.
- Bootstrap sequence (TAD §4.2): on empty `User` table, create `System Administrator` role with all-DocType permissions, generate and log a random admin password, create `admin@localhost`.
- Context injection: Auth Middleware groundwork (`auth.FromContext`) ready for the API layer in Phase 6.

**Dependencies:** Phase 4 (`perm.Engine` must exist to grant `RolePermission` rows meaning; `orjanda-core`'s Documents flow through the full Phase 1–4 pipeline like any Application).

**Completion criteria:**
- [x] Fresh database + `orjanda-core` install → bootstrap creates exactly one admin user, logs a password once, and is idempotent (second startup does not recreate or reset it).
- [x] JWT access token expires at 15 minutes; refresh token rotates on use; a reused (revoked) refresh token is rejected.
- [x] `auth.Provider` is swappable via config without touching `document`, `perm`, or `api` code (interface substitution test with a mock `Provider`).
- [x] A `RolePermission` row change is visible to `perm.Engine.CheckAction` on the next request (no stale-permission caching bug at this layer — caching is introduced later in Phase 12 and must not violate this).

---

### Phase 6 — API Layer ✅ COMPLETE

**Objective:** Full HTTP surface: REST CRUD, RPC custom methods, and the Metadata API, all permission-enforced end to end.

**Source references:** PRD §14 (all), §12.3 (Chi decision); TAD §3.2 (Standard REST Request Flow), §9.2 (`api.MethodHandler`).

**Deliverables:**
- `api` package on Chi: REST handlers for the six operations in PRD §14.2, RPC dispatch for `POST /api/v1/method/{app}.{module}.{method}` with `api.RegisterMethod`/`MethodOpts` (TAD §9.2), Metadata API (`GET /api/v1/meta[/{doctype}[/links]]`) per PRD §14.4 / TAD §6.1's pre-calculated-permissions JSON shape.
- Middleware chain in the exact order of PRD §12.2: CORS → Auth → Rate Limit → Permission → Handler.
- Response envelope (PRD §14.5) applied uniformly, with `errors.Error` → HTTP status/code/message mapping from Phase 0.
- `orjanda.Site` composition root (TAD §12.4) wiring Registry, DB, Permissions, EventBus, Cache into request-scoped handlers.

**Dependencies:** Phase 5 (Auth Middleware needs a working `auth.Provider`); Phase 4 (Permission Middleware needs `perm.Engine`).

**Completion criteria:**
- [x] Full CRUD lifecycle test suite against the Phase 1 reference Application over real HTTP (`httptest`), asserting correct behavior for at least three distinct roles (admin, scoped role, no-access role).
- [x] A custom RPC method respects `AllowedRoles` identically to a Document-level permission check (same `perm.Engine` path, per §3 item 4).
- [x] `GET /api/v1/meta/{doctype}` returns permissions pre-calculated for the calling identity, verified against two different roles returning different `can_*` flags for the same DocType.
- [x] API response time for simple CRUD is < 50ms p95 and Document list (1000 records, paginated) is < 100ms p95 against PostgreSQL (PRD §33.1 targets, first point they are testable end-to-end).

---

### Phase 7 — Agent Runtime: LLM Abstraction & Tool Generation ✅ COMPLETE

**Objective:** The Registry produces correct, permission-aware, per-identity tool definitions, and at least two LLM providers can consume them.

**Source references:** PRD §23 (all), §24 (all), §26 (all); TAD §2.6 + §2.7 (`llm.Provider`, `agent.Tool`), §10 (Agent Tool Generation Algorithm), §8.2 (workflow tool integration), §6.3 (TypeScript SDK's shared field-mapping table, for cross-check only).

**Deliverables:**
- `agent/llm` package: `Provider` interface (full form, TAD §2.7) with OpenAI and Anthropic implementations — `ChatCompletion`, `StreamChatCompletion`, tool-calling, structured-output support, failover/circuit-breaker/token tracking per PRD §26.4.
- `agent/tools` package: `ToolTemplate`, `ToolRegistry.Compile/ForIdentity` implementing the full deterministic algorithm in TAD §10.1–§10.4: per-`CompiledDoc` search/list/read/create/update/delete generation gated by `DocPermission`, one `execute_action_{doctype}` per workflowed DocType (§8.2), field JSON-Schema mapping (§10.2), `GatedFields` compile-time marking + `AllowedFields`-based per-identity projection (§10.3), and `agent_hidden` exclusion.
- `agent.RegisterTool` custom tool registration (PRD §24.3) merged into `ForIdentity`'s output per TAD §10.4.
- `ToolRegistry.Compile` wired into Registry-compile-time (Phase 1's `Compile()` completion hook), per TAD §3.1 step 5.

**Dependencies:** Phase 6 (tool execution ultimately calls the same Document Engine/`perm.Engine` the API layer uses — this phase generates tool *definitions*; Phase 8 wires *execution*). Phase 1 (Registry) and Phase 4 (`perm.Engine.AllowedFields`) are the direct data sources.

**Completion criteria:**
- [x] For the PRD §24.2 `Employee`/`salary` worked example, `ForIdentity` includes `salary` in `create_employee`'s schema for an `hr_manager`-role caller and omits the property entirely for a caller without that role.
- [x] Tool count for a 50-Document reference Registry is `O(len(CompiledDocs))`, not proportional to child-table struct count (TAD §10.1 step 7, directly asserted).
- [x] A workflowed DocType produces exactly one `execute_action_*` tool regardless of transition count.
- [x] Both OpenAI and Anthropic providers pass an identical contract test suite against `Provider` (tool-calling round trip, streaming, structured output where supported).
- [x] Provider failover triggers on a simulated 5xx from the primary provider and completes the request via the fallback (PRD §26.4).

---

### Phase 8 — Agent Runtime: Context, Planning, Safety & Execution

**Objective:** A complete, safe, end-to-end agent turn: discovery → planning → tool execution → approval → audit, indistinguishable in permission/audit behavior from a human-driven API call.

**Source references:** PRD §23.3–§23.4, §25 (all), §27 (all), §28 (all); TAD §3.3 (Agent Execution Flow), §11 (Context Strategy & Planning), §12 (Safety, Approval & Security Contract), §6.2 (WebSocket contract) + §12.3 extension.

**Deliverables:**
- `agent/runtime` package: `Runtime.Execute` main loop (TAD §3.3 / PRD §27.2), Session Manager, Context Manager implementing the discovery/operation tool split (TAD §11.1) and per-turn planning-mode classification (TAD §11.2: ReAct default, Plan-and-Execute escalation on detected step dependencies).
- `agent/planner`: `Plan`/`PlanStep` structured-output contract and whole-plan pre-execution validation against `ToolTemplate.BaseSchema` (TAD §11.3).
- `agent/safety`: `SafetyPolicy`, `SafetyLayer` implementing the exact evaluation order in TAD §12.1 (Always → Bulk → RoleOverride → RequireApproval → AutoApprove, fail-closed default), rate limiting via `cache.Store` (Phase 2), token budget tracking, tool allowlist enforcement (composes with Phase 7's `ForIdentity` filter per TAD §10.3 step 3).
- Agent Executor: routes tool calls through the **same** Document/Workflow Engine entry points used by the API layer (Phase 6) — no separate execution path — and sets the `via_agent`/`agent_session`/`agent_prompt` audit fields (Phase 4's `audit.Log`) unconditionally on every agent-initiated write.
- WebSocket endpoint (`/api/v1/agent/stream`) implementing the client/server message contract in TAD §6.2, including the extended `approval_required` payload with `policy_reason` (TAD §12.3).
- `orjanda agent chat` terminal-mode entry point stub (full CLI packaging happens in Phase 10; the underlying `Runtime.Execute` loop is exercised here).

**Dependencies:** Phase 7 (tool definitions and LLM providers must exist to drive a loop against them); Phase 6 (the Executor calls into the same Document Engine the API layer uses); Phase 2's `cache.Store` (rate limiting).

**Completion criteria:**
- [x] PRD §38.1 (simple query) and §38.2 (multi-step with approval) worked examples pass end-to-end against a `MockLLM`-equivalent harness (full harness formalized in Phase 11; a minimal scripted double is acceptable here to unblock this phase).
- [x] A delete operation is *always* gated by approval regardless of `SafetyPolicy` configuration (TAD §12.1 step 1, cannot be overridden by config in a test that tries).
- [x] A bulk operation exceeding `MaxBulkOperations` requires approval even when its verb is in `AutoApprove` (TAD §12.1 step 2).
- [x] An idle session's first LLM call includes only the fixed discovery tool set (TAD §11.1, tool-count assertion) — session tool count grows only after a `describe_document` or prior tool call references a DocType.
- [x] An invalid `Plan` (bad step 3) executes **zero** side effects — steps 1–2 are not run before validation fails (TAD §11.3).
- [x] Every agent-initiated write is flagged `via_agent=true` in the audit log; every human-initiated write (via Phase 6's API layer) is `via_agent=false`. No code path can mismark either direction.
- [x] A user attempting an operation their role forbids receives a `PermissionDenied` error surfaced to the agent (not silently retried or hidden from the LLM) — mirrors PRD §25.2 exactly.

---

### Phase 9 — Admin UI & TypeScript SDK

**Objective:** A metadata-driven React admin UI, embedded in the Go binary, with a working agent chat panel and generated TypeScript client.

**Source references:** PRD §17 (all), §18 (all); TAD §6.1 (Metadata API shape), §6.3 (TypeScript SDK Generation Contract), §9.1 (`ui.Page`/`Registry`).

**Deliverables:**
- `orjanda-ui` React + Tailwind SPA: `MetaProvider`, `AuthProvider`, `PermissionGuard`, field renderer components, `DocFormPage`/`DocListPage` driven entirely by `/api/v1/meta/{doctype}` (no hardcoded per-Document forms), `DashboardPage`.
- `ComponentRegistry` (PRD §18.2) with the documented resolution order (Document-specific → Application override → Default).
- `ui.Page`/`ui.Registry` backend registration (TAD §9.1) surfaced in the sidebar/menu per PRD §18.3.
- Agent Chat UI component wired to the Phase 8 WebSocket contract, rendering `token`/`tool_start`/`tool_end`/`approval_required` events, including the `policy_reason`-differentiated approval prompt (TAD §12.3).
- `@orjanda/codegen`: build-time pass from `orjanda registry list --json`-shaped output to generated TypeScript interfaces + typed `documents.{DocType}` client (TAD §6.3), regenerated on Registry content-hash change during `orjanda serve`.
- `embed.FS`-based single-binary embedding of the production Vite build (PRD §17.4); Vite dev-server proxy for development (mitigates PRD §40 Risk R7).

**Dependencies:** Phase 6 (Metadata API, REST API), Phase 8 (Agent Chat WebSocket must exist to build the chat panel against).

**Completion criteria:**
- [x] Registering a new Document in the backend (no frontend code change) makes it appear in the sidebar, with a working auto-generated form and list, on next page load (PRD §17.3's explicit non-regression requirement).
- [x] `ComponentRegistry` resolution order is verified with all three override levels registered simultaneously for the same field.
- [x] Agent Chat UI correctly renders a full PRD §38.2-equivalent approval round trip, including Approve/Deny/Modify.
- [x] `@orjanda/codegen` output for the Phase 1 reference Application type-checks (`tsc --noEmit`) and its field types match the Phase 7 agent tool JSON Schema mapping one-for-one (shared table cross-check, TAD §10.2).
- [x] Production build produces a single binary; `go build` with no separate frontend deploy step required.

---

### Phase 10 — CLI & Developer Experience

**Objective:** The full `orjanda` command surface from PRD §21.1, each command mapped to the concrete internals specified in TAD §16.

**Source references:** PRD §21 (all), §37 (Example Developer Workflow, used as the acceptance script); TAD §16 (CLI Command Contract).

**Deliverables:**
- Cobra-based `cmd/orjanda` binary implementing every command in the TAD §16 table: `init`, `new document`, `new module`, `serve`, `migrate diff/up/status`, `console`, `bench`, `install`/`uninstall`, `test`, `agent chat`, `registry list`/`describe`.
- `serve` vs. `bench` behavioral split exactly as TAD §16 specifies: `serve` auto-creates missing tables and warns-and-continues on Registry errors; `bench` never auto-creates and fails fast on any Registry or migration-drift error.
- Code-generation templates for `new document`/`new module`/`init`, producing the exact scaffold shapes shown in PRD §21.3 and §11.1.

**Dependencies:** Phase 9 (`serve` must start the full stack including the Admin UI); Phase 2 (`migrate` subcommands); Phase 8 (`agent chat`).

**Completion criteria:**
- [x] The PRD §37 "Example Developer Workflow" script runs verbatim against the built CLI, end to end, on a clean machine: `init` → four `new document` calls → manual edit → `serve` → agent chat interaction → `migrate diff` → `test`.
- [x] `orjanda bench` refuses to start against a database with pending migrations; `orjanda serve` starts anyway and logs a warning.
- [x] `orjanda registry describe Employee --json` output matches the shape consumed by Phase 9's `@orjanda/codegen`.

---

### Phase 11 — Testing Harness

**Objective:** The `orjanda/testing` package exists as a first-class, documented dependency so every phase above (and every downstream Application) can write the test patterns shown in PRD §32.2–§32.3 directly, without hand-rolled fixtures.

**Source references:** PRD §32 (all); TAD §17 (Testing Harness Contract).

**Deliverables:**
- `orjanda/testing` package: `NewTestSite`, `WithApps`, `WithDialect`, `CreateUser`, `WithUser`, `SeedFixtures`, `MockLLM`/`ToolCall`/`TextResponse`/`ApprovalPrompt` exactly per TAD §17.
- Retrofit: replace the ad hoc test doubles used in Phases 3–8 with `orjanda/testing` where equivalent, so the harness is dogfooded by the framework's own test suite before any external Application depends on it.
- `testcontainers-go`-backed PostgreSQL integration test lane in CI, gated separately from the fast in-memory SQLite unit-test lane.

**Dependencies:** Phase 8 (agent testing needs `MockLLM` against a real `Runtime`); conceptually this phase could start earlier, but is sequenced last among the "backend-complete" phases so it can validate the *entire* stack, not a partial one.

**Completion criteria:**
- [x] PRD §32.2's `TestLeaveRequestCreation` and §32.3's `TestAgentCanSearchEmployees` run verbatim (adjusted only for final package paths) against the finished framework.
- [x] `NewTestSite` provisions a fresh isolated database per test with no cross-test leakage (parallel test run assertion).
- [x] `MockLLM` correctly scripts a full Plan-and-Execute + `ApprovalPrompt` exchange (Phase 8 completion criteria's PRD §38.2 scenario, now expressed as a harness-based test rather than an ad hoc double).

> **Phase 11 done notes:** `testing/` now ships `NewTestSite`, `WithApps`, `WithDocuments`,
> `WithDialect`, `CreateUser`, `WithUser`, `SeedFixtures`, and `MockLLM`/`ToolCall`/`ToolCalls`/
> `TextResponse`/`ApprovalPrompt` per TAD §17. `TestSite` exposes the real Document Engine as
> `site.Document` and an Agent Runtime as `site.Agent`; per-turn LLM/approval injection is
> provided by `agent.WithProvider`/`agent.WithApprovals` (TAD §3.3). The three completion
> criteria are proven by `testing/site_test.go` (PRD §32.2/§32.3 verbatim, parallel isolation,
> Plan-and-Execute + approval round trip). The `testcontainers-go` PostgreSQL lane runs under
> the `integration` build tag (`testing/postgres_integration_test.go`); the CI `integration` job
> gates it away from the fast SQLite unit lane. The ad-hoc doubles in `agent/runtime` and
> `orjanda-core` were deliberately kept: they exercise unit-level concerns (provider transcripts,
> `repeat`/`failNext`, event sinks, concrete `*sqlite.DB`) the app-level harness does not expose,
> and the harness is dogfooded by its own self-tests instead.

---

### Phase 12 — Extension Hardening, Scaffolding & MVP Validation

**Objective:** Close out the remaining MVP-scoped extension points, confirm every stubbed post-MVP hook is genuinely inert by default, build the PRD §36 reference HR Application, and formally validate against PRD §44.4.

**Source references:** PRD §36–§38 (Example Application/Workflows), §44.4 (MVP Validation Criteria); TAD §9.3 (extension resolution table, remaining rows), §15 (Multi-Tenancy Hook Points), §33.2 (Caching Strategy — permission-eval caching), §40 (Key Technical Risks, final review pass).

**Deliverables:**
- Permission-evaluation request-scoped caching (PRD §33.2) added to `perm.Engine`, verified not to violate Phase 5's "no stale permission" completion criterion across requests.
- Multi-tenancy hooks (`TenantScopedDB` decorator, `dal.WithTenantBypass`, conditional `tenant_id` column) implemented per TAD §15, wired but **inactive** (`site.Config.MultiTenant: false` default) — confirms PRD §30.3's "not in MVP scope, but architecture accommodates it" without turning it on.
- `orjanda-app-hr-example`: the full PRD §36.1 reference Application (`Department`, `Employee`, `LeaveType`, `LeaveRequest`, the leave-approval workflow, the two named hooks) built using nothing but the public surface from Phases 1–11 — this is the "dogfood the framework end to end" deliverable and the vehicle for the validation checklist below.
- Final pass against every risk in PRD §40 (R1–R8): confirm each mitigation named in the PRD is actually in place (traceable to a specific deliverable in Phases 0–11) and record the mapping.

**Dependencies:** All prior phases — this is the integration and hardening phase, not new subsystem work.

**Completion criteria (directly restating PRD §44.4, now checkable against a running system):**
- [x] The `orjanda-app-hr-example` Application's four Documents are implemented in ~200 lines of Go (PRD §36.1's estimate, measured with `gocloc` or equivalent as a sanity check, not a hard gate).
- [x] The Admin UI renders correct forms and lists for all four Documents with zero frontend code specific to them.
- [x] The REST API supports full CRUD for all four Documents with authentication and permission enforcement verified for at least two roles.
- [ ] The agent answers "How many employees are in department X?" correctly via live tool calls (not a mock) against a seeded database.
- [ ] The agent creates a leave request end-to-end, including the approval confirmation round trip, against a live LLM provider (OpenAI or Anthropic).
- [ ] The agent is denied access to `Salary` for a role without `hr_manager`, verified both in the tool schema (field absent) and in a direct attempt to read it via `get_employee`.
- [ ] The agent operates on all four Documents using only auto-generated tools — zero Document-specific agent code was written for `Department`, `LeaveType`, or `LeaveRequest` (only the one custom `calculate_leave_balance` tool from PRD §24.3/§37 is hand-written, as the PRD itself specifies).
- [ ] The audit log contains a complete, correctly `via_agent`-flagged trail for every operation performed during this validation pass, human and agent alike.
- [ ] The leave-approval workflow enforces role-gated transitions exactly as declared (a `Department Head`/`HR Manager`-only `Approve` transition rejects an `Employee`-role attempt).
- [ ] Performance targets from PRD §33.1 hold under the reference Application with a seeded dataset of at least 1,000 `Employee` records.

**MVP sign-off:** Phase 12's checklist passing in full **is** the MVP Definition of Done (PRD §44.1's central thesis, validated). No phase after this one is required to ship v1.0 of the MVP.

---

## 5. Phase Dependency Graph (Summary)

```
Phase 0 (Primitives)
   └─▶ Phase 1 (Schema, Registry, App System)
          └─▶ Phase 2 (DAL, Dialects, Migrations, Cache, Search stub, Background stub)
                 └─▶ Phase 3 (Document Engine — bare CRUD)
                        └─▶ Phase 4 (Event/Perm/Workflow/Audit — full Engine integration)
                               └─▶ Phase 5 (Auth, orjanda-core, Bootstrap)
                                      └─▶ Phase 6 (API Layer: REST/RPC/Metadata)
                                             ├─▶ Phase 7 (Agent: LLM + Tool Generation)
                                             │      └─▶ Phase 8 (Agent: Context/Planning/Safety/Execution)
                                             │             └─▶ Phase 9 (Admin UI + TS SDK)
                                             │                    └─▶ Phase 10 (CLI)
                                             │                           └─▶ Phase 11 (Testing Harness)
                                             │                                  └─▶ Phase 12 (Hardening + MVP Validation)
                                             └───────────────────────────────────────────┘
```
Phases 7–11 are drawn as a single chain above because each has a genuine technical dependency on the previous (tool generation needs the Registry and permission engine; execution needs tool generation; the UI needs the agent WebSocket; the CLI needs `serve` to start the full stack; the testing harness validates the whole stack). In practice, once Phase 6 is complete, the *Agent* line (7→8) and a reduced *Admin UI shell* (static pages only, not the agent chat panel) could proceed in parallel with separate engineers, re-converging at Phase 9's agent-chat-panel deliverable — teams may parallelize here at their own risk, but Phase 9's full completion criteria still require Phase 8 to be done first.

---

## 6. Explicitly Out of Scope for This Plan

Restated from PRD §5 (Non-Goals) and §44.3 (MVP Non-Scope) so no phase above is misread as license to build them:

- Field-level permission UI beyond the backend enforcement already in Phase 4 (full editor is v0.2, PRD §44.3).
- Active multi-tenancy (Phase 12 only wires inert hooks; enabling it is v0.2).
- MySQL dialect, OpenTelemetry, durable/active background job processing (all v0.2).
- MCP server interface, Ollama/local LLM provider, visual workflow designer (all v0.3).
- Cross-application schema extension, field-level i18n (v0.4).
- Report builder, dashboard widgets beyond the basic `DashboardPage` shell (v0.5).
- Print formats, PDF generation, email integration (v0.6).
- A specific business application as a product (ERP, CRM, etc.) — the Phase 12 HR Application is a reference/validation artifact only, per PRD NG1.
- A fully autonomous agent with no approval gates (PRD NG3) — Phase 8's Safety Layer is mandatory, not configurable away in MVP.

## 7. Post-MVP Roadmap (Pointer Only)

Sequencing beyond MVP sign-off (Phase 12) is already defined in PRD §43.1's `v0.2`–`v1.0` table and is not reproduced or re-planned here — doing so would introduce scope this plan is explicitly chartered not to add. When post-MVP work begins, it should get its own implementation plan document using this one as a template, seeded from PRD §43 and any TAD sections added at that time.

---

*End of Document*