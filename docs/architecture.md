# Orjanda Architecture

A prose-level tour of how Orjanda is put together — enough to understand the
data flow in about ten minutes. For exact interfaces and contracts, see
`docs/ORJANDA-TAD.md`; for the product rationale, `docs/ORJANDA-PRD.md` §9.
This document complements rather than restates those specs.

## The core idea

You declare a business entity **once**, as a Go struct (a `Document`) with
`oj` struct tags. From that single declaration, Orjanda derives, automatically:

- a **database table** (via the DAL and Migrator),
- a **REST/RPC API** (full CRUD + search for every Document),
- an **admin UI** (metadata-driven list pages and forms),
- **AI-agent tools** (CRUD tools generated per Document from the Registry),

and every one of those surfaces enforces the **same permission rules** through
the same engine. There is no per-entity integration code, and no separate
permission path for the agent (PRD §25.1).

## The pipeline

```
             Go struct + oj tags (a Document)
                          │  Register()
                          ▼
                    ┌─────────────┐
                    │   Registry   │  compiled once at startup; read-only catalog
                    └─────────────┘
                          │  drives every surface
        ┌────────────┬────┴─────┬──────────────┬───────────────┐
        ▼            ▼          ▼              ▼               ▼
   DAL / DB     REST/RPC    Admin UI      Agent tools      Migrator
   (schema →    (CRUD,      (list pages,  (search/list/    (diff/up;
    tables)      search)     forms, chat)  read/create/     table DDL)
                                          update/delete)
        └──────────────────────┬─────────────────────────────┘
                               ▼
                       perm.Engine + audit.Log
                       (every read/write, every surface)
```

## The single binary

Orjanda is a **modular monolith** (PRD §9.1), not microservices. One process
contains:

- the HTTP server (REST + RPC + WebSocket agent stream),
- the embedded Admin UI (a committed Vite production build served via
  `embed.FS` — see `ui_embed.go`; the `spaHandler` routes `/api/*` to the API
  router and everything else to the SPA with an `index.html` fallback),
- the embedded **Agent Runtime**, which calls the exact same
  `document.*`/`workflow.*` functions as the API — there is no separate agent
  backend (PRD §23.1).

## Package layers

The dependency graph is strictly layered, top depends on bottom (TAD §5.1):

```
cmd/orjanda        Main
server             HTTP assembly, UI embedding
agent              Agent Runtime, LLM interactions
api                REST/RPC/WS handlers, middleware
document           Document Engine, validation
workflow perm audit event     business-logic services
dal                query generation, dialect drivers (postgres, sqlite)
schema             Registry, meta types
auth app           identity, JWT; application module system
errors config      primitives
```

Support packages `cache`, `search`, and `background` sit alongside `dal` —
services the Engine and Agent Runtime consume. `cli` is a thin command-dispatch
layer above `cmd/orjanda`'s Main (TAD §5.1). The layering exists to prevent
circular imports and keeps every subsystem swappable behind an interface.

## Request lifecycle

Inbound requests flow through a fixed middleware chain (PRD §12.2) before
reaching handlers:

```
Request → CORS → Auth (JWT → auth.Identity) → Rate Limit → Permission → Handler
```

- `auth.Identity` (user, roles, tenant) travels **only** via `context.Context` —
  never globals, never extra parameters (TAD §1.2).
- The **Permission** middleware calls `perm.Engine.CheckAction` against the
  Document being operated on. Custom RPC methods are gated the same way, as a
  synthetic `DocType = "method:" + name` (TAD §9.2).
- Every mutation shares one `dal.Tx` with its `audit.Entry` — a failed audit
  write rolls back the data write (TAD §13.1).

## Permissions — one engine, one path

RBAC first, then AND-composed ABAC rules (TAD §2.4/§2.7):

- Each Document declares `Permissions` (role → read/write/create/delete) in its
  `DocMeta()`.
- `perm.Engine.CheckAction` runs the document-level RBAC check; any registered
  `perm.Rule` (custom ABAC) evaluates afterward, and all rules must pass.
- The REST layer, RPC layer, agent tools, and workflow transitions all call the
  **same** engine. A hand-rolled check outside it is a defect, not a shortcut
  (PRD §25.1).

## The Agent Runtime

The embedded agent (TAD §11) works like this:

1. **Tool generation** — at startup, the Agent Runtime compiles the Registry
   into one tool per verb per Document (`create_leave_request`,
   `read_employee`, …) with JSON Schemas built from the Document's fields. Tool
   count stays `O(len(CompiledDocs))` regardless of schema size (TAD §10.1).
2. **Discovery vs. operation** — the agent first discovers what it may operate
   on (`registry list`), then calls operation tools; gated-field metadata is
   never leaked to it (TAD §11.1).
3. **Safety layer** — write tools route through approval gates
   (`AlwaysRequireApproval`, bulk-operation limits), and the whole loop runs as
   the caller's identity, so the agent can do nothing the human cannot (TAD
   §12, PRD §28).
4. **Execution** — a ReAct loop (with Plan-and-Execute for larger tasks) calls
   the same `document.*` functions the API calls, then streams back through
   REST or the WebSocket chat.

## Extension points

Eleven places are designed to be extended without forking the framework (TAD
§9). Each is an interface with a documented default:

| Extension point | Interface | MVP default |
|---|---|---|
| Document lifecycle hooks | `event.Handler` | in-process event bus |
| Custom API methods | `api.MethodHandler` | RPC dispatch |
| Permission rules (ABAC) | `perm.Rule` | RBAC + registered rules |
| Field validators | `schema.Validator` | tag-driven |
| Auth providers | `auth.Provider` | built-in JWT |
| LLM providers | `llm.Provider` | OpenAI, Anthropic, OpenAI-compatible |
| Agent tools | `agent.Tool` | auto-generated CRUD tools |
| Search backends | `search.Backend` | dialect FTS (SQLite FTS5 / Postgres `tsvector`) |
| Cache backends | `cache.Store` | in-process LRU |
| Background jobs | `background.Job`/`Queue` | in-memory stub (post-MVP) |
| UI pages | `ui.Page` | metadata-driven registry |

Swap an implementation behind one of these interfaces (e.g. a Redis cache, an
Elasticsearch backend) by configuration — no Document Engine change.

## What is deliberately out of scope

Microservices, runtime plugin loading, multi-tenancy activation, MySQL, and a
few v0.3+ features are designed around, not designed in. The boundaries are
fixed in PRD §5 and §44.3 — requests to activate them are scope changes, not
bugs.

## Where to go deeper

- **Exact interfaces and algorithms** — `docs/ORJANDA-TAD.md` (§5.1 package
  DAG, §9 extension points, §10 tool generation, §11 agent runtime).
- **Product decisions and rationale** — `docs/ORJANDA-PRD.md` (§9 architecture,
  §12 request lifecycle, §23–§25 agent & permissions).
- **Running it** — `docs/getting-started.md`.
