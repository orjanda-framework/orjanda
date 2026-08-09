# Orjanda — Technical Architecture Document (TAD)

**Version:** 1.0.0
**Date:** 2026-08-09
**Status:** Engineering Specification

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
5. **Tool Generation Pass**: The `agent` package queries the locked Registry, generating `create`, `update`, `delete`, `search`, and `list` tools per `CompiledDoc`.
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
   - Writes to Audit Log.
   - Commits Transaction.
6. **REST Handler**: Serializes created document to JSON (after passing through `perm.Engine.FilterRead`).

### 3.3 Agent Execution Flow
1. User sends natural language request via Agent UI.
2. `Agent API` receives the message and delegates to `agent.Runtime`.
3. **Planner** sends LLM prompt with `auth.Identity`-bound available tools (filtered by roles).
4. LLM returns Tool Call (e.g., `create_employee(data)`).
5. **Agent Executor**:
   - Synthesizes an internal request.
   - **Safety Layer Check**: If the tool call requires approval (e.g. create/update/delete), pause execution, send approval payload to UI via WebSocket, and wait.
   - If approved, invokes **Document Engine** directly with the same context.
   - Document Engine flow executes identically to 3.2.
   - Executor traps output or typed errors and converts them to LLM-friendly string observations.

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
  - `{"type": "approval_required", "action_id": "req-123", "details": {"doctype": "Employee", "action": "create", "payload": {...}}}`

---

## 7. Implementation Sequencing (Milestones)

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
