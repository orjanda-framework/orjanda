# Orjanda — Technical Architecture Document (TAD)

**Version:** 1.1.0
**Date:** 2026-08-09
**Status:** Engineering Specification

> **Changelog 1.1.0:** Added a full gap analysis against the PRD (v1.0.0) and formalized every
> extension point, subsystem, and workflow named but not contracted in the original TAD. See
> "Addendum: Gap Analysis and New Formalizations" before §7. No content from 1.0.0 was removed;
> §7 ("Implementation Sequencing") is renumbered to §18 to make room for the new sections.

---

## 1. Foundational Decisions

### 1.1 Error Model
All Orjanda packages must return errors that implement the internal `orjanda/errors` interface. This ensures errors can be mapped to HTTP status codes and handled safely by the Agent LLM without leaking sensitive system details.

```go
package errors

type ErrorCode string

const (
    CodeValidation  ErrorCode = "VALIDATION_ERROR" // HTTP 400
    CodeAuth        ErrorCode = "AUTH_ERROR"       // HTTP 401
    CodePermission  ErrorCode = "PERMISSION_DENIED"// HTTP 403
    CodeNotFound    ErrorCode = "NOT_FOUND"        // HTTP 404
    CodeConflict    ErrorCode = "CONFLICT"         // HTTP 409
    CodeInternal    ErrorCode = "INTERNAL_ERROR"   // HTTP 500
)

type Error interface {
    error
    Code() ErrorCode
    Message() string        // Safe for human/LLM consumption
    Details() map[string]any// E.g., specific field validation failures
    Unwrap() error          // Underlying original error
}
```

### 1.2 Context Propagation
The `context.Context` is the sole mechanism for request-scoped state.
- **Typed Keys**: Unexported custom types are used for context keys to prevent collisions.
- **Required Injections**: 
  - `auth.Identity`
  - `tenant_id` (for post-MVP multi-tenancy compatibility)
  - `request_id` (for tracing and audit logs)
- **Extraction Helpers**: Use package-level extractors (e.g. `auth.FromContext(ctx)`).

### 1.3 Configuration Schema
Configuration is driven by Viper, bound to `orjanda.yaml` and environment variables with an `ORJANDA_` prefix.

```yaml
server:
  port: 8080
  host: "0.0.0.0"
  cors_origins: ["*"]
database:
  driver: "postgres" # postgres | sqlite
  dsn: "postgres://user:pass@localhost:5432/orjanda?sslmode=disable"
  max_open_conns: 25
  max_idle_conns: 5
llm:
  default_provider: "openai"
  providers:
    openai:
      api_key: ${ORJANDA_OPENAI_API_KEY}
      model: "gpt-4o"
      max_tokens: 4096
    anthropic:
      api_key: ${ORJANDA_ANTHROPIC_API_KEY}
      model: "claude-3-5-sonnet-20240620"
      max_tokens: 4096
  safety:
    max_bulk_operations: 5
```

### 1.4 Naming Conventions
- **Struct Tags**: Use `oj` for Orjanda-specific metadata (e.g. `oj:"required,unique"`).
- **Database Tables**: Pluralized, snake_case mapping of the Document Name (e.g. `LeaveRequest` -> `leave_requests`).
- **Database Columns**: snake_case mapping of struct fields (e.g. `FirstName` -> `first_name`).

---

## 2. Core Interfaces

### 2.1 Schema Types

**The Document Contract:**
```go
package schema

type Document interface {
    DocMeta() Meta
    GetID() string
    SetID(string)
    // Getter/Setter for reflection-free map access
    Get(field string) any
    Set(field string, value any) error
}
```

**Compiled Document (Used by Registry):**
```go
package schema

type CompiledDoc struct {
    Name        string
    Module      string
    TableName   string
    Searchable  bool
    Submittable bool
    Icon        string
    Description string
    TitleField  string
    Fields      []Field
    Permissions []DocPermission
    ChildTables []CompiledChild
}

type Field struct {
    Name        string
    DBColumn    string
    Type        FieldType // String, Int, Date, Link, etc.
    Required    bool
    Unique      bool
    Label       string
    Options     []string
    LinkTarget  string
    Hidden      bool
    AgentHint   string
}
```

### 2.2 `schema.Registry`
The read-only compiled metadata store.

```go
package schema

type Registry interface {
    Get(docType string) (*CompiledDoc, error)
    List() []*CompiledDoc
    Relationships(docType string) []Relationship
    // Internal compilation API
    Register(app string, doc Document) error
    Compile() error
}
```

### 2.3 `dal.Database` and `dal.Dialect`
The Database interface abstracts the dialect and standardizes query generation.

```go
package dal

type Database interface {
    Query(ctx context.Context, q Select) ([]map[string]any, error)
    Insert(ctx context.Context, docType string, data map[string]any) (string, error)
    Update(ctx context.Context, docType string, id string, data map[string]any) error
    Delete(ctx context.Context, docType string, id string) error
    Transaction(ctx context.Context, fn func(Tx) error) error
}

type Tx interface {
    Database // Embeds Database to provide the same API within a transaction
    Commit() error
    Rollback() error
}

type Dialect interface {
    CreateTable(doc schema.CompiledDoc) string
    AlterTable(diff schema.SchemaDiff) []string
    SelectSQL(q Select) (string, []any)
    InsertSQL(tableName string, fields map[string]any) (string, []any)
    UpdateSQL(tableName string, id string, fields map[string]any) (string, []any)
    DeleteSQL(tableName string, id string) (string, []any)
    FullTextSearch(tableName string, query string, fields []string) (string, []any)
    Placeholder(n int) string
}

type Select struct {
    DocType string
    Fields  []string
    Filters map[string]any // Advanced filters post-MVP, simple equality for MVP
    OrderBy string
    Limit   int
    Offset  int
}
```

### 2.4 `perm.Engine`
Evaluates access control. Used by API Middleware, Document Engine, and Agent Executor.

```go
package perm

type Engine interface {
    // Evaluates document-level CRUD permissions
    CheckAction(ctx context.Context, docType, action string) error
    
    // Returns allowed fields and filtered data
    FilterRead(ctx context.Context, docType string, data map[string]any) (map[string]any, error)
    FilterWrite(ctx context.Context, docType string, data map[string]any) (map[string]any, error)
}
```

### 2.5 `event.Bus`
In-process, synchronous event dispatcher.

```go
package event

type Bus interface {
    On(docType, eventName string, handler Handler)
    Emit(ctx context.Context, docType, eventName string, doc map[string]any) error
}

type Handler func(ctx context.Context, doc map[string]any) error
```

### 2.6 `agent.Runtime` and `llm.Provider`

```go
package agent

type Runtime interface {
    Execute(ctx context.Context, userMessage string) (*Response, error)
    RegisterTool(tool Tool)
}

type Tool struct {
    Name         string
    Description  string
    Parameters   map[string]any // JSON Schema
    AllowedRoles []string
    Handler      func(ctx context.Context, args map[string]any) (any, error)
}

package llm

type Provider interface {
    ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    SupportsToolCalling() bool
}

type ChatRequest struct {
    Model       string
    Messages    []Message
    Tools       []ToolDefinition
}
```

### 2.7 Interface Extensions (closing gaps against PRD §22–§27)

The abbreviated interfaces in §2.6 omit members the PRD's fuller definitions (§22.1, §26.1) require and that the new sections below (§10–§12) depend on. These are additive — no existing member is removed or renamed.

```go
package llm

type Provider interface {
    ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
    SupportsToolCalling() bool
    SupportsStructuredOutput() bool
    ModelInfo() ModelInfo
}

type ChatRequest struct {
    Model          string
    Messages       []Message
    Tools          []ToolDefinition
    Temperature    float64
    MaxTokens      int
    ResponseFormat *JSONSchemaFormat // non-nil only in Plan-and-Execute mode, §11.3
}

package perm

type Engine interface {
    CheckAction(ctx context.Context, docType, action string) error
    FilterRead(ctx context.Context, docType string, data map[string]any) (map[string]any, error)
    FilterWrite(ctx context.Context, docType string, data map[string]any) (map[string]any, error)

    // AllowedFields projects field-level permission (oj:"permission=role") against the
    // caller's identity WITHOUT requiring an actual data payload — used by ToolRegistry
    // (§10) to build per-identity JSON Schemas for the agent before any record exists.
    AllowedFields(ctx context.Context, docType, action string) ([]string, error)

    // RegisterRule wires a custom ABAC Rule (§9.3) into evaluation, run after the
    // RBAC check in CheckAction passes. Rules compose with AND semantics.
    RegisterRule(r Rule)
}
```

---

## 3. Data Flow Specifications

### 3.1 Registry Compilation Pipeline
1. `app.Install()` calls `schema.Register(doc)`.
2. Framework startup invokes `registry.Compile()`.
3. Iterates over registered types:
   a. Reflects over struct fields.
   b. Parses `oj` tags into `schema.Field` definitions.
   c. Invokes `DocMeta()` to merge explicit metadata.
4. **Relationship Resolution Pass**: Validates all `link` fields point to registered types. Rejects circular child tables.
5. **Tool Generation Pass**: The `agent` package queries the locked Registry, generating `create`, `update`, `delete`, `search`, and `list` tools per `CompiledDoc`. The full deterministic algorithm behind this pass — including field-permission gating and the discovery/operation tool split — is formalized in §10 and §11.
6. Registry becomes strictly read-only.

### 3.2 Standard REST Request Flow (Create)
1. `POST /api/v1/document/Employee`
2. **Auth Middleware**: Extracts JWT, injects `auth.Identity` into Context.
3. **Permission Middleware**: Calls `perm.Engine.CheckAction(ctx, "Employee", "create")`.
4. **REST Handler**: Parses JSON body into `map[string]any`.
5. **Document Engine**:
   - Calls `perm.Engine.FilterWrite` to strip unpermitted fields.
   - Dispatches `before_validate` event.
   - Validates fields against `CompiledDoc` schema (required, format, options).
   - Dispatches `after_validate` event.
   - Begins DB Transaction (`dal.Tx`).
   - Dispatches `before_insert` and `before_save` events. If a hook returns an error, the transaction is aborted.
   - Calls `dal.Tx.Insert()`.
   - Dispatches `after_insert` and `after_save` events.
   - Writes to Audit Log (contract formalized in §13).
   - Commits Transaction.
6. **REST Handler**: Serializes created document to JSON (after passing through `perm.Engine.FilterRead`).

### 3.3 Agent Execution Flow
1. User sends natural language request via Agent UI.
2. `Agent API` receives the message and delegates to `agent.Runtime`.
3. **Planner** sends LLM prompt with `auth.Identity`-bound available tools (filtered by roles).
4. LLM returns Tool Call (e.g., `create_employee(data)`).
5. **Agent Executor**:
   - Synthesizes an internal request.
   - **Safety Layer Check**: If the tool call requires approval (e.g. create/update/delete), pause execution, send approval payload to UI via WebSocket, and wait. Approval evaluation logic is formalized in §12.
   - If approved, invokes **Document Engine** directly with the same context.
   - Document Engine flow executes identically to 3.2.
   - Executor traps output or typed errors and converts them to LLM-friendly string observations.

### 3.4 Workflow Transition Flow
1. `POST /api/v1/method/core.workflow.execute_action` (or the agent's `execute_action_{doctype}` tool, §10) with `{doctype, id, action}`.
2. Routed to `workflow.Engine.Execute` — full contract in §8. This reuses `perm.Engine.CheckAction`-equivalent role checks and the same `dal.Tx` transaction pattern as §3.2; it is not a parallel code path.
3. On success, an `audit.Entry{Action: "workflow_transition"}` is written (§13) and `event.Bus.Emit(ctx, docType, "on_workflow_transition", doc)` fires, which downstream hooks (e.g. leave balance deduction, PRD §19.3) subscribe to exactly like any other lifecycle event.

### 3.5 Migration Flow
`orjanda migrate diff` → `dal.Migrator.Diff` → `dal.Migrator.Write` → (review) → `orjanda migrate up` → `dal.Migrator.Up`. Full contract in §14.

---

## 4. Core Application Schema (`orjanda-core`)

The `orjanda-core` application provides foundational administrative documents. It bootstraps automatically if no users exist.

### 4.1 Core Documents
```go
type User struct {
    schema.BaseDocument
    Email    string `oj:"required,unique,format=email,searchable"`
    FullName string `oj:"required,searchable"`
    Password string `oj:"hidden"` // Brcypt hash
    Roles    []UserRole `oj:"child_table"`
    Active   bool   `oj:"default=true"`
}

type UserRole struct {
    schema.BaseChild
    Role schema.Link `oj:"link=Role,required"`
}

type Role struct {
    schema.BaseDocument
    RoleName string `oj:"required,unique"`
}

type RolePermission struct {
    schema.BaseDocument
    Role   schema.Link `oj:"link=Role,required"`
    DocType string     `oj:"required"`
    Read   bool
    Write  bool
    Create bool
    Delete bool
    Submit bool
}
```

### 4.2 Bootstrap Sequence
On application startup, if the `User` table is empty:
1. Create `System Administrator` Role.
2. Grant `System Administrator` all permissions on all registered DocTypes.
3. Generate a secure random password and log it to stdout.
4. Create an `admin@localhost` User with the `System Administrator` role.

---

## 5. Dependency Graph and Initialization

### 5.1 Package DAG
To prevent circular imports, the dependency hierarchy is strictly layered (top depends on bottom):

1. `cmd/orjanda` (Main)
2. `server` (HTTP assembly, UI embedding)
3. `agent` (Agent Runtime, LLM interactions)
4. `api` (REST/RPC Handlers)
5. `document` (Document Engine, Validation)
6. `workflow`, `perm`, `audit`, `event` (Business logic services)
7. `dal` (Query generation, Dialect Drivers)
8. `schema` (Registry, Meta types)
9. `auth` (Identity types, JWT parsing)
10. `errors`, `config` (Primitives)

New packages introduced in this revision slot into the existing DAG without altering it: `app` sits alongside `auth` at layer 9 (both are consumed by `server` and `api`, neither depends on `document`); `cache`, `search`, `background` sit alongside `dal` at layer 7 (services the Document Engine and Agent Runtime consume but that do not themselves depend on `document`); `cli` sits above `cmd/orjanda`'s Main as a thin command-dispatch layer with no business logic.

### 5.2 Initialization Order
Inside `orjanda.NewSite()`:
1. Load Config.
2. Initialize Database connection pool.
3. Initialize Event Bus, Audit Log, and Cache.
4. Initialize Schema Registry.
5. Applications register Documents to Registry.
6. **Compile Registry** (lock schemas, resolve relationships).
7. Initialize DAL and Document Engine.
8. Initialize Permission Engine.
9. Initialize Agent Runtime (triggers tool generation from Registry).
10. Run migrations / Bootstrap if requested.
11. Mount API routes and attach Middlewares.
12. Start HTTP server.

### 5.3 Initialization Order — Additions from This Revision
The steps above are unchanged; the following components attach at the indicated existing step rather than introducing new numbered steps, to keep `NewSite()`'s call order stable across revisions:

| Component | Attaches at step | Notes |
|---|---|---|
| `cache.Store` | 3 | Backs Registry/permission caching (§ Performance, PRD §33.2) |
| `search.Backend` | 7 | Defaults to the active `Dialect.FullTextSearch`; see §9.3 |
| `background.Queue` | 3 | In-memory stub in MVP; see §9.3 |
| `app.Dependency` DAG resolution | 5 | Reorders per-Application registration before Registry.Compile; see §7 |
| `workflow.Engine` | 7 | Registered alongside `perm`/`audit`/`event` at DAG layer 6 |
| `agent.ToolRegistry.Compile` | 9 | Runs as part of Agent Runtime init, per §10 |
| `ui.Registry` | 11 | Collects `ui.Page` registrations before routes mount |

---

## 6. Frontend Contract

### 6.1 Metadata API JSON Shape
`GET /api/v1/meta/{doctype}`

```json
{
  "name": "Employee",
  "title_field": "FirstName",
  "searchable": true,
  "fields": [
    {
      "name": "FirstName",
      "type": "string",
      "label": "First Name",
      "required": true,
      "options": null,
      "link": null
    }
  ],
  "permissions": {
    "can_read": true,
    "can_write": true,
    "can_create": false,
    "can_delete": false
  }
}
```
*Note: Permissions are pre-calculated for the requesting user so the UI can hide/disable buttons immediately.*

### 6.2 Agent Chat WebSocket
For real-time streaming of LLM tokens, tool execution status, and human-in-the-loop approvals.
- **Endpoint**: `WS /api/v1/agent/stream`
- **Client → Server**: 
  - `{"type": "message", "text": "Create a leave request"}`
  - `{"type": "approval_response", "action_id": "req-123", "approved": true}`
- **Server → Client**:
  - `{"type": "token", "content": "I "}`
  - `{"type": "tool_start", "tool": "create_employee"}`
  - `{"type": "tool_end", "tool": "create_employee", "success": true}`
  - `{"type": "approval_required", "action_id": "req-123", "details": {"doctype": "Employee", "action": "create", "payload": {...}}}` — the full, extended payload shape (including `policy_reason`) is specified in §12.

### 6.3 TypeScript SDK Generation Contract (formalizes PRD §22.2)
The `@orjanda/react` package's typed `documents.{DocType}` client (PRD §22.2 example) is generated, not hand-written, by a build-time codegen step:

1. `orjanda registry list --json` (§16) is invoked against a running or ephemerally-started dev site, producing the full `CompiledDoc[]` payload identical in shape to `GET /api/v1/meta`.
2. A codegen pass (`@orjanda/codegen`, a Node script shipped with the framework, not a Go binary) maps each `CompiledDoc` to:
   - A TypeScript `interface {DocType}` from `Fields` (per the same `Field Types` table used for agent tool schemas, PRD §10.3 — one shared mapping table, not two).
   - A typed client object `documents.{DocType}` with `list/get/create/update/delete` methods, each a thin `fetch` wrapper against the REST endpoints in §14.2 of the PRD.
3. Codegen output is written to `orjanda-ui/src/generated/` and is regenerated automatically by `orjanda serve`'s dev-mode file watcher whenever the compiled Registry changes (detected via a Registry content hash), giving the "new Document is immediately available" guarantee (PRD §17.3) at the TypeScript layer as well as the rendered-UI layer.
4. Post-MVP (PRD §22.3): the same codegen pass targets Python and other languages by swapping the output template; the `CompiledDoc[]` input contract does not change.

---

## Addendum: Gap Analysis and New Formalizations

This revision reviewed the TAD (v1.0.0, §1–§7 above) against every section of the PRD (v1.0.0, §1–§44). Sections §1–§7 above are the original, unmodified content plus targeted cross-references inserted where a new section now supplies detail that was previously only sketched (e.g. §3.1 step 5, §3.3 step 5). The table below is the review record: what the PRD specified that the TAD had not yet turned into an interface, algorithm, or contract, and where that gap is now closed.

| PRD § | Topic | Prior TAD coverage | Closed in |
|---|---|---|---|
| §11 | Application & Module System (`app.Definition`, lifecycle, hook conflict order) | Named only in the Package DAG (§5.1) | §7 |
| §19.3 | Workflow state machine | Only lifecycle *events* were contracted (§2.5); no `workflow.Definition`/`Engine` | §8 |
| §20.2 | 11 extension points | 4 of 11 had interfaces (`event.Handler`, `perm` implied, `llm.Provider`, `agent.Tool`) | §9 |
| §15.1 | `auth.Provider` | Named in PRD prose only | §9.1 |
| §33.2 | Cache interface | Referenced as "the cache interface" with no definition | §9.1 (`cache.Store`) |
| Architecture diagram (§9), §20.2 | Search backend, Background job engine | Present in the box diagram only | §9.1 (`search.Backend`, `background.Job`/`Queue`) |
| §24 | Automatic tool generation pipeline | One-line pipeline sketch (old §3.1 step 5) | §10 |
| §23.4 | Two-phase (discovery/operation) context strategy | Named, not specified | §11.1 |
| §27.1–27.3 | Hybrid ReAct + Plan-and-Execute switching, structured output validation | Decision recorded in PRD only | §11.2–§11.3 |
| §25.3, §28 | Agent security controls table; approval policy | Listed as a table with no mechanism; WebSocket payload sketch only | §12 |
| §29.1–29.2 | Audit entry write path, observability metrics | Go struct shown, no `Log` interface or write-path guarantee | §13 |
| §13.4 | Atlas + Goose migration pipeline | Named tools, five-step prose workflow, no interface | §14 |
| §30 | Multi-tenancy hook points | `Tenant` field on `Identity` only | §15 |
| §21 | CLI command set | Listed as a bare command list in the PRD | §16 |
| §32.2–32.3 | `orjanda/testing` package | Usage examples only (PRD code snippets), no package contract | §17 |
| §22.2 | TypeScript SDK generation | Example client code, no generation pipeline | §6.3 |

Sections §7–§17 below are new. §18 is the original §7 ("Implementation Sequencing"), renumbered and extended with one additional milestone (§18, item 7) that sequences the delivery of everything formalized in this addendum.

---

## 7. Application & Module System Contract

Formalizes PRD §11 (Application and Module System) and §34.2 (Extension Principles).

```go
package app

type Definition struct {
    Name         string
    Title        string
    Version      string
    Description  string
    Publisher    string
    Modules      []Module
    Dependencies []Dependency
}

type Module struct {
    Name  string
    Title string
}

type Dependency struct {
    App        string
    MinVersion string
}

// Applications opt into lifecycle phases (PRD §11.3) by implementing these
// optional interfaces on their app.Definition's associated init type. A
// Definition with none of these is valid — Install/Upgrade/Uninstall become
// no-ops beyond the framework's own Document/table registration.
type Installable interface {
    OnInstall(ctx context.Context, site *orjanda.Site) error
}
type Upgradable interface {
    OnUpgrade(ctx context.Context, site *orjanda.Site, fromVersion string) error
}
type Uninstallable interface {
    OnUninstall(ctx context.Context, site *orjanda.Site, dropTables bool) error
}
```

### 7.1 Registration, Ordering, and Conflict Resolution
1. Applications are registered via `site.Install(app.Definition)` calls in `main.go` (PRD §20.3 example), in whatever order the developer writes them.
2. **Dependency Resolution Pass** (runs at Initialization step 5, §5.2): the framework builds a DAG from every `Definition.Dependencies` entry and topologically sorts it. This resolved order — not the `main.go` call order — governs Document registration and hook registration. A cycle is a fatal startup error (`ErrCircularAppDependency`, `errors.CodeInternal`).
3. **Hook Conflict Rule** (closes PRD §34.2 item 5, "later-installed application wins"): for hooks registered by different Applications on the same `{DocType, event}` pair, execution order equals the dependency-resolved install order — a dependency's hooks run before its dependent's hooks, so the dependent can observe/override state the dependency already set. Hooks within the same Application run in source declaration order.
4. **Enable/Disable** (PRD §11.3): modeled as a row in an `orjanda-core` `InstalledApplication` Document, read once during Registry compilation (Initialization step 6). A disabled Application's Documents are skipped for that compilation pass — their tables and data are retained, not dropped — so re-enabling requires no migration.
5. **Cross-application hooks** (PRD §34.2 item 3, "Payroll can add hooks to Employee"): any Application may call `event.Bus.On("Employee", ...)` regardless of which Application registered the `Employee` Document; the Dependency Resolution Pass is what makes the ordering of such cross-cutting hooks deterministic rather than import-order-dependent.

### 7.2 Module Scoping
Modules carry no lifecycle of their own — they are a Registry-level grouping tag (`CompiledDoc.Module`) consumed by: the Admin UI sidebar grouping (PRD §17), `list_document_types()` filtering (§11.1 below), and the Role permission editor's grouping. A Module is not a package boundary; Go package layout under `modules/{name}/` (PRD §11.1) is a filesystem convention only, not enforced by the Registry.

---

## 8. Workflow Engine Contract

Formalizes PRD §19.3 (Workflow State Machine), previously represented in the TAD only via generic lifecycle events (§2.5).

```go
package workflow

type Definition struct {
    DocType      string
    States       []State
    Transitions  []Transition
    OnTransition map[string]Handler // keyed by destination State.Name
}

type State struct {
    Name  string
    Style string // UI hint only: "gray" | "yellow" | "green" | "red" | a hex color
}

type Transition struct {
    From         string
    To           string
    Action       string    // verb surfaced as a UI button label and an agent enum value
    AllowedRoles []string
    Guard        GuardFunc // optional; evaluated after the AllowedRoles check
}

type GuardFunc func(ctx context.Context, doc map[string]any) error
type Handler func(ctx context.Context, doc map[string]any) error

type Engine interface {
    Register(def Definition) error
    AvailableTransitions(ctx context.Context, docType, currentState string) []Transition
    Execute(ctx context.Context, docType, id, action string) error
}
```

### 8.1 Execution Contract
1. Registering a `Definition` for a DocType causes `Registry.Compile()` to add a `WorkflowState string` field to that DocType's `CompiledDoc.Fields` automatically — analogous to how `BaseDocument.DocStatus` is auto-added for Submittable Documents (PRD §10.2), but keyed by name rather than a fixed int enum, since workflow states are developer-defined.
2. `Execute` resolves the target Document's current `WorkflowState`, then looks up the `Transition` matching `{From: currentState, Action: action}`. No match → `errors.CodeConflict` with `Message()` = "no such transition from current state" (safe to surface to the agent/LLM directly, per the Error Model in §1.1).
3. Role check: `AllowedRoles` is evaluated through the **same** `perm.Engine` code path used by the Document Engine and Agent Executor (union-of-roles semantics, not AND) — this is the concrete mechanism behind PRD §19.3's "the agent checks... the user has the required role" and PRD §25's "no privilege escalation" guarantee applied to workflows specifically.
4. If present, `Guard` runs inside the same `dal.Tx` as the state write, so a Guard that inspects related Documents (e.g. "does the employee have enough leave balance") sees a consistent snapshot.
5. The Engine writes the new `WorkflowState`, dispatches `event.Bus.Emit(ctx, docType, "on_workflow_transition", doc)`, then invokes `OnTransition[To]` if registered (this is where PRD §19.3's leave-balance-deduction example hook attaches).
6. Commit. An `audit.Entry{Action: "workflow_transition", Changes: [{Field: "WorkflowState", OldValue, NewValue}]}` is written (§13) inside the same transaction.

### 8.2 Agent Integration
The Tool Generation algorithm (§10) emits exactly **one** `execute_action_{doctype}` tool per workflowed DocType — not one tool per `Transition` — whose `action` parameter's JSON Schema `enum` is populated at call time from `AvailableTransitions(ctx, docType, doc.WorkflowState)`, already filtered to the calling identity's roles. This is what keeps tool count bounded regardless of how many states/transitions a workflow defines (ties into the two-phase context strategy, §11), and is the concrete implementation behind PRD §38.2's "Tool call: execute_action(... action='Submit')" step.

---

## 9. Extension Point Interfaces

Formalizes the full PRD §20.2 extension-point table. Four of eleven rows already had interfaces in TAD v1.0.0 (`event.Handler`, `perm.Engine` implicitly, `llm.Provider`, `agent.Tool`); the remaining seven are defined here.

### 9.1 New Interfaces

```go
package schema

type Validator interface {
    Validate(ctx context.Context, field Field, value any) error
}
// Registered via struct tag oj:"validator=Name" + schema.RegisterValidator(name, v)
// at package init time, mirroring how event hooks self-register (PRD §19.2).

package auth

type Provider interface {
    ValidateToken(ctx context.Context, token string) (*Identity, error)
    GetUserInfo(ctx context.Context, token string) (*UserInfo, error)
}
// The built-in JWT provider (§1.3, §4) is itself just the default Provider;
// wrapping OAuth2/OIDC, LDAP, or SAML means implementing this interface and
// setting site.Config.Auth.Provider — no other framework code changes.

package perm

type Rule interface {
    // Evaluated after the RBAC document-level check in CheckAction passes.
    // Return nil to allow, a *errors.Error{Code: CodePermission} to deny.
    // Multiple registered Rules compose with AND semantics.
    Evaluate(ctx context.Context, check Check) error
}

package search

type Backend interface {
    Index(ctx context.Context, docType, id string, fields map[string]any) error
    Remove(ctx context.Context, docType, id string) error
    Search(ctx context.Context, docType, query string, limit int) ([]string, error) // matching IDs
}
// MVP default: satisfied by a thin adapter over the active dal.Dialect's
// FullTextSearch (PRD §13.3) — no separate index process. The interface exists
// so a post-MVP Elasticsearch/Meilisearch backend is a config swap
// (site.Config.Search.Backend), not a Document Engine change.

package cache

type Store interface {
    Get(ctx context.Context, key string) ([]byte, bool, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
}
// MVP default: in-process LRU, no external dependency. Registry metadata
// caching and per-request permission-check caching (PRD §33.2) both go
// through this interface exclusively, so a Redis-backed Store is a drop-in
// replacement when horizontally scaling (PRD §33.3).

package background

type Job interface {
    Name() string
    Handle(ctx context.Context, payload []byte) error
}
type Queue interface {
    Enqueue(ctx context.Context, job string, payload []byte, opts EnqueueOpts) error
    RegisterHandler(job string, j Job)
}
type EnqueueOpts struct {
    RunAt      time.Time // zero value = run immediately
    MaxRetries int
}
// Background jobs are explicitly post-MVP (PRD §44.3). The interface is
// defined now so Application authors can code against a stable contract; the
// MVP binary wires a non-durable, in-memory Queue implementation. A durable
// (DB- or Redis-backed) Queue is a v0.2 swap behind the same interface.

package ui

type Page struct {
    Path      string
    Title     string
    Component string // JS module path resolved by the frontend bundle loader
    Icon      string
    Menu      string
}
type Registry interface {
    RegisterPage(p Page)
    Pages() []Page
}
```

### 9.2 Custom RPC Methods
PRD §14.3 shows `api.RegisterMethod` but the TAD had not stated its signature:

```go
package api

type MethodHandler func(ctx context.Context, args map[string]any) (any, error)

type MethodOpts struct {
    AllowedRoles []string
    HTTPMethod   string // "GET" | "POST"
}

func RegisterMethod(name string, h MethodHandler, opts MethodOpts)
```
`AllowedRoles` here is enforced through the same `perm.Engine.CheckAction`-style path (as a synthetic `DocType = "method:" + name`), not a bespoke check — consistent with §25's "no separate agent/API permission path" guarantee.

### 9.3 Resolution Table (PRD §20.2, with concrete identifiers)

| Extension Point | Interface | Registered via |
|---|---|---|
| Document hooks | `event.Handler` | `event.Bus.On` |
| Custom API methods | `api.MethodHandler` | `api.RegisterMethod` |
| Permission rules | `perm.Rule` | `perm.Engine.RegisterRule` (§2.7) |
| Field validators | `schema.Validator` | `oj:"validator=Name"` + `schema.RegisterValidator` |
| Auth providers | `auth.Provider` | `site.Config.Auth.Provider` |
| LLM providers | `llm.Provider` | `site.Config.LLM.Providers[name]` |
| Agent tools | `agent.Tool` | `agent.Runtime.RegisterTool` |
| Search backends | `search.Backend` | `site.Config.Search.Backend` |
| Cache backends | `cache.Store` | `site.Config.Cache.Store` |
| Background jobs | `background.Job` | `background.Queue.RegisterHandler` |
| UI pages | `ui.Page` | `ui.Registry.RegisterPage` |

---

## 10. Agent Tool Generation Algorithm

Formalizes PRD §24 (Automatic Agent Capability Generation), expanding the one-line "Tool Generation Pass" sketch in §3.1 step 5 into a deterministic, testable algorithm.

```go
package agent

type ToolTemplate struct {
    DocType     string
    Verb        string // "search"|"list"|"read"|"create"|"update"|"delete"|"execute_action"
    Name        string // snake_case(Verb) + "_" + snake_case(DocType), e.g. "create_employee"
    BaseSchema  map[string]any // JSON Schema built from ALL fields, including gated ones
    GatedFields []string       // fields whose oj:"permission=role" tag is non-empty
}

type ToolRegistry interface {
    // Runs once, immediately after Registry.Compile() (Initialization step 9, §5.3).
    Compile(reg schema.Registry) error

    // Runs once per Agent Runtime turn, during context assembly (PRD §23.3 step 3d).
    // Projects each ToolTemplate through the caller's permissions to produce the
    // tool list actually sent to the LLM for this turn.
    ForIdentity(ctx context.Context, id auth.Identity) []llm.ToolDefinition
}
```

### 10.1 Compile-Time Generation Algorithm
For each `CompiledDoc` where the Document itself is not `oj:"agent_hidden"` (a schema-level tag, distinct from the per-field `hidden` tag in PRD §10.4):

1. If `Searchable`: emit a `search_{doctype}` template.
2. Always emit `list_{doctype}` and `get_{doctype}` (read tools require no permission gate beyond the document-level Read check, evaluated at `ForIdentity` time).
3. If any `DocPermission` grants `Create`: emit `create_{doctype}`; `required` params are fields where `Required && !Hidden`.
4. If any `DocPermission` grants `Write`: emit `update_{doctype}`.
5. If any `DocPermission` grants `Delete`: emit `delete_{doctype}`, flagged `AlwaysRequireApproval` in the Safety Layer (§12) unconditionally — this flag is set at generation time, not left to the default Safety Policy config, matching PRD §28.1's "Delete operations: Always require approval, Configurable: No".
6. If a `workflow.Definition` targets this DocType: emit exactly one `execute_action_{doctype}` template per §8.2, with an empty compile-time `enum` populated per-call.
7. Child-table fields (`oj:"child_table"`) do **not** get their own tools; they are only reachable nested inside the parent's `create_{doctype}`/`update_{doctype}` payload, exactly as PRD §10.1's `Skills []EmployeeSkill` is nested in the `Employee` create call. This keeps tool count at `O(len(CompiledDocs))`, not `O(all struct types including children)`.
8. For each `api.RegisterMethod`-registered RPC method with a non-empty `AllowedRoles`: emit one tool named after the method (dots replaced with underscores, e.g. `hr_leave_get_balance`).

### 10.2 Field Schema Mapping
Each `Field` maps to JSON Schema per the Field Types table in PRD §10.3 (`schema.Currency` → `{"type":"number"}`, `schema.Link` → `{"type":"string","description":"Reference to a {LinkTarget} document"}`, `schema.Date` → `{"type":"string","format":"date"}`, etc.). This is the **same** mapping table consumed by the TypeScript codegen in §6.3 — one canonical field-type-to-external-representation table, not two independently maintained ones. `agent_hint` (PRD §24.4) is appended verbatim to the field's `description`.

### 10.3 Per-Identity Projection
A field is added to `GatedFields` at compile time iff its `oj:"permission=role"` tag is non-empty; `BaseSchema` always includes it, keeping `ToolTemplate` identity-independent and safely cacheable in the Registry. `ForIdentity` then, per call:
1. Calls `perm.Engine.AllowedFields(ctx, docType, verb)` (§2.7).
2. Deletes any `GatedFields` property not present in the returned set from a copy of `BaseSchema`, and removes it from `required` if listed.
3. Applies the Safety Layer's `ToolAllowlist` (§12) as a final filter.

This closes PRD §24.2's worked example precisely: `create_employee`'s `salary` property exists in `BaseSchema` but is deleted by `ForIdentity` for any caller without the `hr_manager` role — the LLM never sees the field *name* for those users, which is a stronger guarantee than merely omitting its value.

### 10.4 Custom Tools
`agent.Tool` registrations (PRD §24.3) bypass §10.1–§10.3 entirely: they are merged into the same per-identity list by `ForIdentity`, filtered only by their own declared `AllowedRoles`, and are exempt from the `agent_hidden`/`GatedFields` machinery since their `Parameters` are hand-authored, not Registry-derived.

---

## 11. Agent Context Strategy & Planning Mode Selection

Formalizes PRD §23.4 (Schema Context Optimization) and §27 (Agent Planning and Execution Model), neither of which had an algorithmic specification in TAD v1.0.0 beyond the high-level flow in §3.3.

### 11.1 Discovery vs. Operation Tool Split
`ToolRegistry.ForIdentity` always includes a fixed **discovery** set — `list_document_types`, `describe_document`, `list_relationships` — which are hand-written (not `ToolTemplate`-generated) and read directly from `schema.Registry`, gated only by the `agent_hidden` document-level flag.

**Operation** tools (`search_*`, `list_*`, `get_*`, `create_*`, `update_*`, `delete_*`, `execute_action_*`) are attached to the outgoing LLM request lazily by the Context Manager: only DocTypes that have appeared in this session's transcript — either as the argument to a prior `describe_document` call, or as the DocType of any prior tool call/result — get their operation tools included. This is the concrete mechanism behind PRD §23.4's two-phase strategy and directly mitigates Risk R4 (PRD §40): an idle session's first LLM call carries ~3 tool schemas regardless of whether the Registry holds 5 or 500 Documents.

### 11.2 Planning Mode Switch
`agent.Runtime.Execute` classifies each turn before its first LLM call of that turn:
1. **ReAct mode** (default): the user message maps to a bounded, non-dependent sequence of tool calls (e.g. "how many employees are in Engineering" → one `search_employee` call). Runs the loop already specified in §3.3 / PRD §27.2 directly.
2. **Plan-and-Execute mode**: triggered when the LLM's first response for this turn contains ≥2 tool calls where a later call's arguments reference an earlier call's result (a data dependency) — e.g. PRD §38.2's "get employee → calculate balance → create → submit" chain. The Planner then:
   a. Requests a structured `Plan` from the LLM (§11.3).
   b. If **any** step's verb falls in the Safety Layer's `RequireApproval` or `AlwaysRequireApproval` sets (§12), presents the whole plan as one summarized confirmation to the user (matching PRD §38.2's single combined confirmation, not one prompt per step); read-only plans execute silently.
   c. Executes steps sequentially, feeding each result into the next step's argument resolution, validating each step's `Args` against its `ToolTemplate.BaseSchema` (§10) before that step runs.
3. Mode is re-evaluated every iteration of the loop, not fixed for the session — a ReAct session can escalate mid-conversation if a subsequent LLM tool call introduces a dependency chain the initial classification didn't have.

### 11.3 Structured Output Contract
In Plan-and-Execute mode, the Planner sets `llm.ChatRequest.ResponseFormat` (§2.7) to constrain the LLM's output to:

```go
type Plan struct {
    Steps []PlanStep
}
type PlanStep struct {
    Operation string         // a tool name from this turn's ForIdentity() result
    Args      map[string]any
    DependsOn []int          // indices of prior steps this step's Args reference
}
```

Every `PlanStep.Args` is validated against its `Operation`'s projected `ToolTemplate.BaseSchema` (post `ForIdentity` filtering) **before any step executes**. An invalid plan — an unknown `Operation`, a missing required field, a `DependsOn` cycle — is rejected wholesale and returned to the LLM as a single correction turn rather than partially executed. This extends PRD §27.3's "framework validates structured output... before execution" from a single operation to whole-plan validation, and is the guardrail that prevents a plan with a bad step 3 from having already executed steps 1–2 with real side effects.

---

## 12. Agent Safety, Approval & Security Contract

Formalizes PRD §25.3 (Agent-Specific Security Controls) and §28 (Human Approval / Safety Model), both of which were tables/prose in the PRD with no TAD-side mechanism.

```go
package agent

type SafetyPolicy struct {
    AutoApprove             []string          // verbs, e.g. "read","search","list"
    RequireApproval         []string          // e.g. "create","update","submit"
    AlwaysRequireApproval   []string          // e.g. "delete","cancel" — not overridable
    MaxBulkOperations       int
    RequireApprovalForRoles map[string][]string
    RateLimit               RateLimit
    TokenBudgetPerSession   int
    ToolAllowlist           []string          // empty = every generated + custom tool
}

type RateLimit struct {
    OperationsPerMinute int
    Scope               string // "user" | "session"
}

type SafetyLayer interface {
    // Called by the Executor before every tool invocation (§3.3 step 5).
    RequiresApproval(ctx context.Context, id auth.Identity, toolName string, args map[string]any) bool
    CheckRateLimit(ctx context.Context, id auth.Identity) error
    CheckTokenBudget(ctx context.Context, sessionID string, projected int) error
    IsToolAllowed(toolName string) bool
}
```

### 12.1 Approval Evaluation Order
`RequiresApproval` evaluates in this order; the first match wins (fail-closed default at the end):
1. `AlwaysRequireApproval` — checked first; cannot be bypassed by policy config, role, or the compile-time `delete_*` flag from §10.1 step 5.
2. **Bulk check**: if the call's target record count — read from a preceding `list`/`search` result already in the session transcript, or an explicit array argument — exceeds `MaxBulkOperations`, approval is required regardless of verb (PRD §28.1: "Bulk operations (>5 records): Always require approval, Configurable: No" — the `Always` here is enforced by this step running before the role/verb checks below).
3. `RequireApprovalForRoles[callerRole]` — a per-role override, e.g. Interns confirming everything (PRD §28.3).
4. `RequireApproval` — the configured default set for the verb.
5. `AutoApprove` — everything else. An unrecognized verb defaults to requiring approval, never to auto-approval.

### 12.2 Security Control Mechanisms (PRD §25.3 table, made concrete)

| Control | Mechanism |
|---|---|
| Rate limiting | `SafetyLayer.CheckRateLimit`, backed by `cache.Store` (§9.1) sliding-window counters keyed `ratelimit:{scope}:{id}` |
| Scope restriction | A `RequireApprovalForRoles` entry covering all verbs, or a Registry-level `DocPermission{Read:false,...}` for full exclusion |
| Audit flag | Set unconditionally by the Agent Executor on every Document Engine call it makes — never by the Document Engine itself, so human-initiated writes are never mismarked `via_agent` |
| Token budget | `SafetyLayer.CheckTokenBudget`, checked before each LLM call using `ChatResponse.Usage` accumulated on the `Session` |
| Tool allowlist | `SafetyLayer.IsToolAllowed`, applied inside `ToolRegistry.ForIdentity` (§10.3 step 3) as the final filter, after permission projection |
| Sensitive field masking | `oj:"agent_hidden"` on a *field* — distinct from the Document-level `agent_hidden` (§10.1) and from `permission=role` (§10.3) — excluded from `BaseSchema` at compile time for every identity, not filtered per-call |

### 12.3 Approval Payload (extends §6.2)
```json
{"type":"approval_required","action_id":"req-123","details":{
  "doctype":"LeaveRequest","action":"create",
  "payload":{"employee":"EMP-001","leave_type":"Annual","from_date":"2026-08-15","to_date":"2026-08-16"},
  "policy_reason":"RequireApproval"
}}
```
`policy_reason` is one of `AlwaysRequireApproval | BulkLimit | RoleOverride | RequireApproval`, corresponding to the four branches in §12.1, so the UI can render distinct copy (e.g. bulk operations surface the affected record count; `AlwaysRequireApproval` on a delete cannot be dismissed with a "don't ask again" option).

---

## 13. Audit & Observability Contract

Formalizes PRD §29 (Auditability and Observability). The PRD defined the `AuditEntry` Go struct directly; the TAD had referenced "Audit Log" as a step in §3.2 without a package contract or write-path guarantee.

```go
package audit

type Entry struct {
    ID           string
    Timestamp    time.Time
    UserID       string
    DocType      string
    DocID        string
    Action       string
    Changes      []FieldChange
    ViaAgent     bool
    AgentSession string
    AgentPrompt  string
    IPAddress    string
    UserAgent    string
    RequestID    string // correlates to the context.Context request_id, §1.2
}

type FieldChange struct {
    Field    string
    OldValue any
    NewValue any
}

type Log interface {
    Write(ctx context.Context, e Entry) error
    Query(ctx context.Context, f QueryFilter) ([]Entry, error)
}

type QueryFilter struct {
    DocType  string
    DocID    string
    UserID   string
    ViaAgent *bool
    Since    time.Time
    Limit    int
}
```

### 13.1 Write-Path Guarantee
The Audit Log write happens **inside** the same `dal.Tx` as the triggering Document Engine (§3.2) or Workflow Engine (§8.1) operation. A failed audit write aborts the whole transaction — the operation itself is rolled back rather than committing with a missing audit trail. This guarantees the log can never diverge from the data it describes; the trade-off (audit writes contend for the same transaction/lock as the data write) is accepted at MVP scale per PRD §33.1's performance targets.

### 13.2 Diff Computation
`Changes` is computed by the Document Engine comparing the pre-image — fetched inside the same transaction, before the `Update`/`Delete` SQL runs — against the post-write values, field by field in `CompiledDoc.Fields` order. Unchanged fields are omitted from `Changes` entirely (not included with identical Old/New values).

### 13.3 Observability Metrics
PRD §29.2's metrics table (token usage, tool call count, permission denials, approval rate, LLM latency, error rate) is emitted as `slog` structured events with a fixed key set, so a log-based pipeline (Loki/Promtail or similar) can extract metrics without custom parsing — no metrics backend (Prometheus, OTel) ships in MVP, consistent with PRD §29.4 deferring OpenTelemetry:

```go
slog.Info("agent.session.metric", "session_id", sessionID, "metric", "token_usage", "value", usage.TotalTokens)
slog.Info("agent.session.metric", "session_id", sessionID, "metric", "tool_call_count", "value", callCount)
slog.Warn("perm.denied", "user", id.UserID, "doctype", docType, "action", action, "via_agent", viaAgent)
slog.Info("agent.approval", "session_id", sessionID, "policy_reason", reason, "approved", approved)
slog.Info("llm.call", "provider", providerName, "duration_ms", elapsed, "error", errStr)
```

---

## 14. Migration Pipeline Contract

Formalizes PRD §13.4 (Migration System). The PRD named Atlas and Goose and described a five-step prose workflow; no Go interface existed in TAD v1.0.0.

```go
package dal

type Migrator interface {
    // Diff compares the compiled Registry against the live DB schema
    // (introspected via the active Dialect) and returns forward-only changes.
    Diff(ctx context.Context, reg schema.Registry) (*SchemaDiff, error)
    // Write persists Diff's output as a versioned migration file under
    // migrations/, in Goose's up/down SQL format.
    Write(diff *SchemaDiff, dir string) (filename string, err error)
    // Up applies all pending migration files via Goose against the DSN.
    Up(ctx context.Context, dir string) error
    Status(ctx context.Context, dir string) ([]MigrationStatus, error)
}

type SchemaDiff struct {
    CreateTables []schema.CompiledDoc
    AlterTables  []TableAlteration
}

type TableAlteration struct {
    TableName    string
    AddColumns   []schema.Field
    DropColumns  []string // requires --allow-destructive, see §14 step 2
    AlterColumns []ColumnAlteration
}
```

### 14.1 Pipeline (concretizes PRD §13.4's five steps)
1. `orjanda migrate diff` invokes `Migrator.Diff`, which uses Atlas's Go SDK internally to compute the delta between `reg.List()` — translated to Atlas's schema representation via the active `Dialect` — and the introspected live schema.
2. Destructive changes (`DropColumns`, dropped tables) are computed but excluded from the written file unless the CLI is invoked with `--allow-destructive` (§16); without the flag, `Migrator.Write` errors and prints the destructive statements for manual review. This is the concrete mechanism behind PRD §13.5's "forward-only migrations" principle — destructive intent must be explicit, not silently generated.
3. `Migrator.Write` renders the diff as SQL via Goose's file-naming convention (`{timestamp}_{description}.sql`) with `-- +goose Up`/`-- +goose Down` markers. The `Down` section contains only a comment in MVP (`-- down migrations are not generated; author manually if needed`), matching PRD §13.5's "rollbacks are new forward migrations" stance rather than auto-generating (possibly incorrect) reverse DDL.
4. `orjanda migrate up` calls `Migrator.Up`, a thin wrapper over `goose.Up(db, dir)`.
5. **Multi-database awareness** (PRD §13.5): when `site.Config` targets more than one dialect, `Write` runs once per dialect, producing `{timestamp}_{description}.postgres.sql` and `{timestamp}_{description}.sqlite.sql`; `Up` selects the file matching the active `database.driver`.
6. **Data migrations** (PRD §13.4, "useful for data migrations that require business logic"): any `.go` file under `migrations/` implementing `func Up(ctx context.Context, tx dal.Tx) error` is treated as a migration step and interleaved with SQL files by timestamp.

---

## 15. Multi-Tenancy Hook Points

Formalizes PRD §30 (Multi-Tenancy Considerations). Explicitly **not active in MVP** per PRD §30.3 — this section documents where the hooks live so the eventual v0.2 implementation (PRD §43.1) requires wiring, not a rewrite.

1. `auth.Identity.Tenant` (already declared, §4.1) is populated by the active `auth.Provider` (§9.1) at token validation time; it is the empty string whenever `site.Config.MultiTenant` is `false` (the MVP default).
2. `dal.Select.Filters` gains a silently-injected `tenant_id = ?` predicate inside every `dal.Database` operation whenever `site.Config.MultiTenant` is `true` — implemented as a decorator, `TenantScopedDB`, wrapping the base `dal.Database` implementation. The query builder and dialect adapters themselves stay tenant-agnostic; tenancy is a wrapper concern, not a `Dialect`-level one.
3. `schema.CompiledDoc` gains a `tenant_id` column during `Registry.Compile()` **only** when `site.Config.MultiTenant` is `true` — `Dialect.CreateTable` checks this flag, so the same `CompiledDoc` struct definitions serve both single- and multi-tenant deployments without a schema fork.
4. Cross-tenant bypass (admin-only, PRD §30.2) is a `context.Context` marker, `dal.WithTenantBypass(ctx)`, that `TenantScopedDB` checks before injecting the filter. Setting it requires `perm.Engine.CheckAction(ctx, "*", "tenant_bypass")` — a synthetic action granted only to `System Administrator` in the Bootstrap Sequence (§4.2), never to a per-tenant role.

---

## 16. CLI Command Contract

Formalizes PRD §21 (CLI and Developer Experience), which listed commands without specifying what each maps to internally.

| Command | Underlying call | Key flags |
|---|---|---|
| `orjanda init <name>` | Scaffolds `go.mod` + `main.go` importing `orjanda-core` | `--module` (Go module path) |
| `orjanda new document <name>` | Writes `documents/{snake}.go` from a `text/template` scaffold | `--module`, `--submittable` |
| `orjanda new module <name>` | Creates `modules/{name}/{documents,hooks,workflows,api,ui}/` | — |
| `orjanda serve` | `orjanda.NewSite` → `Registry.Compile` → dev-only auto-`CreateTable` for missing tables → `server.Run`; Registry compile errors **warn and continue** serving unaffected Documents | `--port`, `--config` |
| `orjanda migrate diff` | `dal.Migrator.Diff` + `Write` (§14) | `--allow-destructive`, `--dialect` |
| `orjanda migrate up` | `dal.Migrator.Up` | `--dir` |
| `orjanda migrate status` | `dal.Migrator.Status` | — |
| `orjanda console` | REPL wrapping the constructed `*orjanda.Site` (Go-expression evaluator) | — |
| `orjanda bench` | Production entrypoint: no auto-create, requires pre-applied migrations, `Registry.Compile` failure is **fatal** (not warn-and-continue) | `--config` |
| `orjanda install <app>` / `uninstall <app>` | `app.Installable` / `Uninstallable` lifecycle hooks (§7) | `--drop-tables` (uninstall only) |
| `orjanda test` | `go test ./...` with `ORJANDA_ENV=test`, routing `orjanda/testing.NewTestSite` (§17) to an ephemeral SQLite DB | `-run` (passthrough) |
| `orjanda agent chat` | Terminal-mode `agent.Runtime.Execute` loop against the local site; prints `tool_start`/`tool_end` inline instead of over the WebSocket (§6.2) | `--user` (impersonate) |
| `orjanda registry list` / `describe <doc>` | `schema.Registry.List()` / `Get()`, pretty-printed; `--json` feeds the TypeScript codegen pipeline (§6.3) | `--json` |

`serve` vs. `bench` is the concrete distinction behind PRD §21.1's implied dev/production split: `serve` favors fast iteration (auto-create, warn-and-continue on schema errors), `bench` favors fail-fast production safety (refuses to start on any Registry or migration-drift error).

---

## 17. Testing Harness Contract

Formalizes PRD §32.2 (Application Testing) and §32.3 (Agent Testing), which showed usage examples (`ntest.NewTestSite`, `ntest.MockLLM`) without a package contract.

```go
package testing // imported as orjanda/testing

type TestSite struct {
    *orjanda.Site
}

type Option func(*testSiteConfig)

func NewTestSite(t *testing.T, opts ...Option) *TestSite
func WithApps(apps ...app.Definition) Option
func WithDialect(d string) Option // default: in-memory SQLite

func (s *TestSite) CreateUser(t *testing.T, email string, roles ...string) auth.Identity
func (s *TestSite) WithUser(id auth.Identity) context.Context
func (s *TestSite) SeedFixtures(t *testing.T, path string) // Application fixture JSON, PRD §11.1

// MockLLM implements llm.Provider with a scripted response sequence, so agent
// tests are deterministic and require no network access or API keys — the
// concrete form of PRD §32.1's "Mock LLM provider" row.
func MockLLM(t *testing.T, steps ...MockStep) llm.Provider
func ToolCall(name string, args map[string]any) MockStep
func TextResponse(text string) MockStep
func ApprovalPrompt() MockStep // scripts the client-side approval round-trip, §12.3
```

### 17.1 Guarantees
1. Each `NewTestSite` call gets a fresh in-memory SQLite database by default; `WithDialect("postgres")` opts into a `testcontainers-go`-backed instance for dialect-specific tests (PRD §32.1's Integration test row).
2. `Registry.Compile()` has already run when `NewTestSite` returns — Documents supplied via `WithApps` are immediately usable, with no `serve`-equivalent startup step required in the test itself.
3. No HTTP server is started by `NewTestSite`. `document.Create/Read/Update/Delete` are called directly against the Document Engine, bypassing `api/` entirely — this is what lets `TestLeaveRequestCreation` (PRD §32.2) exercise validation, hooks, and permissions without a network round-trip. API-layer tests instead wrap `server.Assemble(site)` in `net/http/httptest.NewServer` (PRD §32.1's API test row).
4. `MockLLM`'s steps are consumed in call order by successive `ChatCompletion` invocations. A test asserting multi-turn behavior — including an approval round-trip or a Plan-and-Execute sequence (§11.2) — chains multiple `MockStep`s, using `ApprovalPrompt()` to script the client's approve/deny response inline with the rest of the scripted exchange.

---

## 18. Implementation Sequencing (Milestones)

*(Originally §7 in TAD v1.0.0; renumbered here. Items 1–6 are unchanged from that version. Item 7 is new in this revision and sequences the delivery of §7–§17.)*

1. **M1: Core Primitives & Registry**
   - Implement `schema`, `errors`, and `config`.
   - Build Registry compilation pipeline.
   - *Test: Define dummy documents, ensure metadata compiles and relationships resolve.*

2. **M2: Data Access & Document Engine**
   - Implement `dal` query builder and SQLite/PostgreSQL dialects.
   - Implement `document` engine (CRUD operations without permissions).
   - *Test: Create, Read, Update, Delete records via Engine directly.*

3. **M3: Business Logic Services**
   - Implement `event` bus, `workflow` engine, and `perm` engine.
   - Integrate them into the Document Engine execution flow.
   - *Test: Hooks fire on save, permissions block unauthorized writes.*

4. **M4: API & Auth**
   - Implement `auth` (JWT) and `api` (Chi router, REST generation, Middlewares).
   - Implement `orjanda-core` (User/Role management and Bootstrap).
   - *Test: Full HTTP CRUD lifecycle with varying user roles.*

5. **M5: Agent Runtime**
   - Implement `agent` tool generation from Registry.
   - Implement LLM provider abstraction and ReAct loop with safety gates.
   - *Test: CLI agent interacts with test DB using auto-generated tools.*

6. **M6: Admin UI**
   - Implement React metadata-driven form/list renderer.
   - Integrate WebSocket agent chat and approval flows.
   - *Test: End-to-end browser interaction.*

7. **M7: Formalized Extension Surface (this revision)**
   - Implement `app.Definition` dependency resolution and lifecycle hooks (§7); `workflow.Engine` (§8); the seven remaining extension interfaces — `schema.Validator`, `auth.Provider`, `perm.Rule`, `search.Backend`, `cache.Store`, `background.Job`/`Queue`, `ui.Page` (§9).
   - Implement `ToolRegistry.Compile`/`ForIdentity` (§10), the discovery/operation split and ReAct/Plan-and-Execute switch (§11), and `agent.SafetyLayer` (§12) — these three together replace the placeholder "Tool Generation Pass" and "Safety Layer Check" steps referenced informally in M5.
   - Implement `audit.Log` (§13) and `dal.Migrator` (§14).
   - Implement the `cli` command table (§16) and `orjanda/testing` package (§17) end-to-end.
   - Stub only (interface defined, no-op/in-memory default implementation, inactive by config default): `background.Queue`, the `TenantScopedDB` decorator (§15) — both remain post-MVP per PRD §44.3.
   - *Test: workflow transitions respect `AllowedRoles` via the shared `perm.Engine` path; `ToolRegistry.ForIdentity` strips `GatedFields` per caller role and respects `ToolAllowlist`; `migrate diff`/`up` round-trip correctly against both dialects; `orjanda/testing.MockLLM` drives a full Plan-and-Execute + approval scenario end-to-end without a live LLM.*