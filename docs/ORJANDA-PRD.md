# Orjanda — Product Requirements Document

**Version:** 1.0.0
**Date:** 2026-08-09
**Status:** Draft — Engineering Specification
**Classification:** Open Source Framework

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Vision](#2-vision)
3. [Problem Statement](#3-problem-statement)
4. [Goals](#4-goals)
5. [Non-Goals](#5-non-goals)
6. [Target Developers and Users](#6-target-developers-and-users)
7. [Core Concepts](#7-core-concepts)
8. [Framework Philosophy](#8-framework-philosophy)
9. [High-Level Architecture](#9-high-level-architecture)
10. [Metadata / Data Model](#10-metadata--data-model)
11. [Application and Module System](#11-application-and-module-system)
12. [Backend Architecture](#12-backend-architecture)
13. [Database Architecture](#13-database-architecture)
14. [API Architecture](#14-api-architecture)
15. [Authentication and Authorization](#15-authentication-and-authorization)
16. [Permissions Model](#16-permissions-model)
17. [Admin UI Architecture](#17-admin-ui-architecture)
18. [UI Customization and Extension Model](#18-ui-customization-and-extension-model)
19. [Workflow / Event System](#19-workflow--event-system)
20. [Plugin / Extension System](#20-plugin--extension-system)
21. [CLI and Developer Experience](#21-cli-and-developer-experience)
22. [SDK Strategy](#22-sdk-strategy)
23. [Agent-Native Architecture](#23-agent-native-architecture)
24. [Automatic Agent Capability Generation](#24-automatic-agent-capability-generation)
25. [Agent Permission and Security Model](#25-agent-permission-and-security-model)
26. [LLM Provider Abstraction](#26-llm-provider-abstraction)
27. [Agent Planning and Execution Model](#27-agent-planning-and-execution-model)
28. [Human Approval / Safety Model](#28-human-approval--safety-model)
29. [Auditability and Observability](#29-auditability-and-observability)
30. [Multi-Tenancy Considerations](#30-multi-tenancy-considerations)
31. [Internationalization / Localization](#31-internationalization--localization)
32. [Testing Strategy](#32-testing-strategy)
33. [Performance and Scalability](#33-performance-and-scalability)
34. [Extensibility Strategy](#34-extensibility-strategy)
35. [Ecosystem Strategy](#35-ecosystem-strategy)
36. [Example Application Architecture](#36-example-application-architecture)
37. [Example Developer Workflow](#37-example-developer-workflow)
38. [Example Agent Workflow](#38-example-agent-workflow)
39. [Competitive / Architectural Comparison](#39-competitive--architectural-comparison)
40. [Key Technical Risks](#40-key-technical-risks)
41. [Important Trade-offs](#41-important-trade-offs)
42. [Recommended Technology Choices](#42-recommended-technology-choices)
43. [Future Evolution](#43-future-evolution)
44. [MVP Scope](#44-mvp-scope)

---

## 1. Executive Summary

Orjanda is an open-source, agent-native business application framework written in Go. It provides a metadata-driven foundation for building administrative and business applications — ERP, CRM, HR, accounting, inventory, project management, and arbitrary custom systems — where AI agent capabilities are an intrinsic part of the framework rather than an external integration.

A developer defines business entities (Documents) through structured schema declarations. From these declarations, Orjanda automatically generates database tables, CRUD APIs, validation, permissions enforcement, administrative UI, search, audit trails, and — critically — AI agent tool definitions. An AI agent embedded in the framework can immediately operate on any registered entity, respecting the same permission and validation rules as human users, without the developer writing per-entity agent code.

The framework targets a developer experience comparable in productivity to Frappe and Django, but is purpose-built for Go's type system, modern software architecture patterns, and the operational reality that AI agents are first-class consumers of business applications.

**Central thesis:** If a business entity exists in Orjanda, it is automatically understandable and operable by an AI agent. The MVP must prove this thesis with a minimal but technically complete implementation.

---

## 2. Vision

Orjanda is the framework developers reach for when they need to build a business application that is both human-operable and agent-operable from day one.

Today, building a business application with agent capabilities requires: (1) building the application, (2) building an API layer, (3) building agent tooling for each entity, (4) synchronizing permissions, (5) maintaining schema consistency across all layers. This is an O(n) problem per entity that creates drift, security gaps, and maintenance burden.

Orjanda collapses this to O(1): define the entity once, and the framework handles all layers — including the agent layer — automatically.

In three years, Orjanda should be:
- The default choice for Go developers building administrative applications.
- A framework whose agent-native architecture is the primary differentiator against traditional frameworks.
- An ecosystem with community-contributed applications (HR modules, inventory systems, etc.) that are inherently agent-operable.

---

## 3. Problem Statement

### 3.1 The Boilerplate Problem
Business applications share 80% of their infrastructure: CRUD, forms, lists, permissions, search, audit. Developers rebuild this for every project. Frameworks like Frappe and Django Admin solve this for Python, but no equivalent exists for Go.

### 3.2 The Agent Integration Problem
Adding AI agent capabilities to an existing application is a parallel engineering effort. Each entity requires: a tool definition, input validation schema, permission mapping, response formatting, and error handling. This tooling drifts from the source schema, creates security surface area, and scales linearly with entity count.

### 3.3 The Permission Synchronization Problem
Agent systems frequently bypass application-level permissions because the agent tooling is built as a separate layer. When permissions change in the application, the agent layer must be updated independently — and frequently is not.

### 3.4 The Go Ecosystem Gap
Go has no metadata-driven business application framework. Developers building admin panels in Go use generic CRUD generators or manually wire together routers, ORMs, and admin templates. There is no Go equivalent to Frappe's "define a DocType, get everything."

---

## 4. Goals

| ID | Goal | Success Metric |
|---|---|---|
| G1 | A developer can define a business entity and immediately get DB, API, UI, and agent capabilities | < 5 minutes from entity definition to working agent interaction |
| G2 | Agent capabilities respect the same permission system as the human UI | Zero permission bypass paths in architecture review |
| G3 | Framework supports multiple database engines | PostgreSQL and SQLite working in MVP |
| G4 | Developer experience is comparable to Frappe/Django for common tasks | Measured by lines-of-code for equivalent CRUD applications |
| G5 | Multiple LLM providers are supported through a single abstraction | At minimum OpenAI and Anthropic |
| G6 | The framework is extensible without forking | Plugin/hook system allows behavior modification at defined extension points |
| G7 | Applications built on Orjanda are production-grade | Proper auth, audit, validation, error handling in generated code |

---

## 5. Non-Goals

| ID | Non-Goal | Rationale |
|---|---|---|
| NG1 | Build a specific business application (ERP, CRM, etc.) | Orjanda is a framework, not a product |
| NG2 | Replace general-purpose web frameworks (Gin, Echo) | Orjanda is for business/admin applications specifically |
| NG3 | Provide a fully autonomous AI agent | The agent assists and executes within defined guardrails |
| NG4 | Support non-Go backend languages | Go is the primary language; this is a deliberate constraint |
| NG5 | Build a low-code/no-code platform | Orjanda targets developers who write code |
| NG6 | Implement a visual workflow designer in MVP | Workflows are code-defined in MVP; visual tooling is future |
| NG7 | Multi-region deployment orchestration | Infrastructure concerns are outside framework scope |

---

## 6. Target Developers and Users

### 6.1 Primary: Go Backend Developers
Developers building internal business tools, administrative systems, or line-of-business applications in Go. They want the productivity of Frappe/Django without switching to Python. They are comfortable with code-first development and CLI workflows.

### 6.2 Secondary: Full-Stack Teams
Teams with Go backend expertise and frontend capability who want to build complete business applications. They value generated admin UI but need the ability to customize and extend it.

### 6.3 Tertiary: Platform Engineers
Engineers deploying and operating Orjanda-based applications. They need multi-tenancy, observability, and security guarantees.

### 6.4 End Users
Business users (non-developers) who interact with Orjanda-based applications through the admin UI or through the AI agent interface (natural language queries, commands, and workflows).

---

## 7. Core Concepts

### 7.1 Document
The fundamental unit in Orjanda. A Document is a declared business entity — analogous to a Frappe DocType or a Django Model — but designed for Go's type system. Every Document has a schema, lifecycle, permissions, and automatically generated capabilities.

### 7.2 Schema
The structured declaration of a Document's fields, types, constraints, relationships, and metadata annotations. Schemas are the single source of truth from which all layers (DB, API, UI, Agent) are derived.

### 7.3 Application
A distributable package of related Documents, business logic, UI customizations, and configuration. An Application is the unit of modularity — analogous to a Frappe App or Django App.

### 7.4 Module
A logical grouping of Documents within an Application. Modules organize Documents by business domain (e.g., an HR Application might have modules for Recruitment, Payroll, and Leave Management).

### 7.5 Registry
The runtime catalog of all registered Documents, their schemas, relationships, hooks, and capabilities. The Registry is the central nervous system that the Agent Runtime, API layer, and UI layer all query.

### 7.6 Agent Runtime
The embedded AI agent subsystem that automatically generates tool definitions from the Registry and executes operations on behalf of users, subject to the same permission and validation rules as the human-facing layers.

### 7.7 Hook
A developer-defined function that executes at a specific point in a Document's lifecycle (before_save, after_insert, on_submit, etc.). Hooks are the primary mechanism for adding custom business logic.

### 7.8 Workflow
A state-machine definition that governs a Document's lifecycle transitions, approval chains, and automated actions. Workflows are declared, not imperative.

---

## 8. Framework Philosophy

### 8.1 Convention over Configuration
Orjanda provides strong defaults. A Document definition with zero configuration should produce a working application with DB, API, UI, and agent capabilities. Configuration is for overriding defaults, not enabling basics.

### 8.2 Metadata as the Single Source of Truth
The Document Schema drives all layers. There is no separate API schema, no separate agent tool schema, no separate UI model. Everything derives from the same declared source.

### 8.3 Agent-Native, Not Agent-Bolted
The agent layer is not an afterthought. The Registry, permission system, and operation execution pipeline are designed so that agents and humans are both first-class consumers. An operation executed by an agent follows the identical code path as one triggered by a human through the UI or API.

### 8.4 Strongly Typed, Code-First
Schemas are defined as Go code (structs + annotations), not as YAML/JSON configuration files. This provides compile-time safety, IDE support, and code review compatibility.

**Decision: Code-First vs. Metadata-Driven**
- *Frappe's approach*: JSON metadata stored in the database, editable at runtime. Enables hot-reloading and non-developer customization.
- *Django's approach*: Python model classes, requiring code changes and migrations. Provides IDE support and version control.
- *Orjanda's decision*: **Code-first with runtime metadata compilation.** Schemas are Go structs annotated with struct tags and method implementations. At startup, the framework compiles these into an in-memory metadata registry. This provides Go's compile-time type safety and IDE integration, while the runtime registry enables dynamic introspection by the Agent Runtime.
- *Rationale*: Go's type system is too valuable to bypass with runtime JSON. Business applications require version-controlled schema changes that go through code review. The agent layer needs runtime introspection, which the compiled registry provides without sacrificing type safety.

### 8.5 Escape Hatches at Every Layer
Generated code is the starting point, not the ceiling. Every layer — API, UI, agent tools, validation — supports developer overrides. The framework should never force a developer to fight the framework to implement a requirement.

### 8.6 Secure by Default
Generated APIs enforce authentication. Generated agent tools enforce permissions. Audit logging is automatic. Developers must explicitly disable security features, not enable them.

---

## 9. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Client Layer                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐  │
│  │  Admin UI     │  │  Custom UI   │  │  Agent Chat Interface    │  │
│  │  (React +     │  │  (Developer  │  │  (Natural Language →     │  │
│  │   Tailwind)   │  │   Built)     │  │   Agent Runtime)         │  │
│  └──────┬───────┘  └──────┬───────┘  └────────────┬─────────────┘  │
│         │                 │                        │                │
└─────────┼─────────────────┼────────────────────────┼────────────────┘
          │                 │                        │
          ▼                 ▼                        ▼
┌─────────────────────────────────────────────────────────────────────┐
│                       API Gateway Layer                             │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  HTTP Router (Chi)                                           │   │
│  │  ├── REST API  (/api/v1/document/{doctype}/{name})          │   │
│  │  ├── RPC API   (/api/v1/method/{app}.{module}.{method})     │   │
│  │  ├── Agent API (/api/v1/agent/chat, /agent/execute)         │   │
│  │  └── Auth Middleware → Permission Middleware → Handler       │   │
│  └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          ▼                    ▼                    ▼
┌──────────────────┐ ┌──────────────────┐ ┌──────────────────────────┐
│  Document Engine │ │  Agent Runtime   │ │  Background Job Engine   │
│  ────────────────│ │  ────────────────│ │  ────────────────────────│
│  • CRUD Ops      │ │  • LLM Gateway   │ │  • Job Queue             │
│  • Validation    │ │  • Tool Registry │ │  • Scheduled Tasks       │
│  • Hooks         │ │  • Planner       │ │  • Async Operations      │
│  • Workflows     │ │  • Executor      │ │  • Retries               │
│  • Permissions   │ │  • Context Mgr   │ │                          │
│  • Search        │ │  • Safety Layer  │ │                          │
└────────┬─────────┘ └────────┬─────────┘ └────────────┬─────────────┘
         │                    │                         │
         ▼                    ▼                         ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     Core Services Layer                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐  │
│  │  Registry     │  │  Permission  │  │  Event Bus               │  │
│  │  (Schemas,    │  │  Engine      │  │  (Hooks, Notifications,  │  │
│  │   Metadata)   │  │  (RBAC+ABAC) │  │   Workflow Triggers)     │  │
│  └──────────────┘  └──────────────┘  └──────────────────────────┘  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐  │
│  │  Audit Log    │  │  Cache       │  │  File Storage            │  │
│  │              │  │              │  │                          │  │
│  └──────────────┘  └──────────────┘  └──────────────────────────┘  │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     Data Access Layer (DAL)                          │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  Query Builder → Dialect Adapter → Database Driver           │   │
│  │  (Supports: PostgreSQL, SQLite; extensible to MySQL, etc.)  │   │
│  └──────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

### 9.1 Architectural Style: Modular Monolith

**Decision:** Orjanda is a modular monolith, not a microservices framework.

**Alternatives considered:**
- *Microservices:* Excessive operational complexity for the target use case (business applications). Introduces network boundaries between tightly coupled business entities. Deployment friction for small/medium teams.
- *Serverless-first:* Incompatible with the embedded agent runtime and in-process event bus. Cold starts are unacceptable for interactive agent sessions.
- *Modular monolith:* Single deployable binary. Clear internal module boundaries via Go packages and interfaces. Can extract services later if scale demands it. Matches Frappe's deployment model. Suits Go's compilation model (single binary).

**Rationale:** Business applications have inherently coupled entities (Invoice → Customer → Product). Forcing these across network boundaries creates latency, consistency problems, and operational burden. A modular monolith with clean internal interfaces provides the right balance for the target developer profile.

---

## 10. Metadata / Data Model

### 10.1 Schema Declaration

Documents are defined as Go structs implementing the `orjanda.Document` interface:

```go
package hr

import "github.com/orjanda-framework/orjanda/schema"

type Employee struct {
    schema.BaseDocument

    FirstName    string              `oj:"required,label=First Name,searchable"`
    LastName     string              `oj:"required,label=Last Name,searchable"`
    Email        string              `oj:"required,unique,format=email"`
    Department   schema.Link         `oj:"link=Department,label=Department"`
    Status       string              `oj:"options=Active|Inactive|On Leave,default=Active"`
    JoinDate     schema.Date         `oj:"required,label=Join Date"`
    Salary       schema.Currency     `oj:"precision=2,permission=hr_manager"`
    Skills       []EmployeeSkill     `oj:"child_table,label=Skills"`
}

func (e Employee) DocMeta() schema.Meta {
    return schema.Meta{
        Name:        "Employee",
        Module:      "HR",
        Searchable:  true,
        Submittable: false,
        Icon:        "users",
        Description: "Company employee record",
        TitleField:  "FirstName + ' ' + LastName",
        SortField:   "LastName",
        SortOrder:   schema.Ascending,
        Permissions: []schema.DocPermission{
            {Role: "HR Manager", Read: true, Write: true, Create: true, Delete: true},
            {Role: "Employee", Read: true, Write: false, Create: false, Delete: false,
                Match: schema.OwnerMatch},
        },
    }
}

type EmployeeSkill struct {
    schema.BaseChild
    SkillName   string `oj:"required,label=Skill"`
    Proficiency string `oj:"options=Beginner|Intermediate|Expert,default=Beginner"`
}
```

### 10.2 BaseDocument Fields (Automatic)

Every Document automatically includes:

| Field | Type | Purpose |
|---|---|---|
| `ID` | `string` (ULID) | Primary key, globally unique, sortable |
| `Name` | `string` | Human-readable identifier (auto-generated or user-defined) |
| `Owner` | `string` | User who created the record |
| `CreatedAt` | `time.Time` | Creation timestamp |
| `UpdatedAt` | `time.Time` | Last modification timestamp |
| `ModifiedBy` | `string` | User who last modified |
| `DocStatus` | `int` | 0=Draft, 1=Submitted, 2=Cancelled (for Submittable docs) |
| `Deleted` | `bool` | Soft-delete flag |

### 10.3 Field Types

| Orjanda Type | Go Type | DB Mapping | Agent Interpretation |
|---|---|---|---|
| `string` | `string` | `VARCHAR/TEXT` | Free text |
| `int`, `int64` | `int`, `int64` | `INTEGER/BIGINT` | Numeric |
| `float64` | `float64` | `REAL/DOUBLE` | Numeric |
| `bool` | `bool` | `BOOLEAN` | Yes/No |
| `schema.Date` | `time.Time` | `DATE` | Calendar date |
| `schema.DateTime` | `time.Time` | `TIMESTAMP` | Date and time |
| `schema.Currency` | `decimal.Decimal` | `NUMERIC` | Monetary value |
| `schema.Text` | `string` | `TEXT` | Long text |
| `schema.RichText` | `string` | `TEXT` | Formatted content |
| `schema.Link` | `string` | `VARCHAR + FK` | Reference to another Document |
| `schema.DynamicLink` | `string` | `VARCHAR` | Polymorphic reference |
| `schema.Attachment` | `string` | `VARCHAR` | File reference |
| `schema.JSON` | `json.RawMessage` | `JSONB/TEXT` | Structured data |
| `[]ChildType` | `[]struct` | Child table | Nested sub-records |

### 10.4 Struct Tag Annotations (`oj` tag)

| Annotation | Meaning |
|---|---|
| `required` | Field must have a value |
| `unique` | Unique constraint |
| `searchable` | Indexed for full-text search |
| `label=X` | Human-readable label for UI and agent |
| `options=A\|B\|C` | Enumerated values |
| `default=X` | Default value |
| `format=email\|url\|phone` | Validation format |
| `link=DocType` | Foreign key to another Document |
| `child_table` | Embedded child table |
| `precision=N` | Decimal precision |
| `permission=role` | Field-level permission |
| `hidden` | Hidden from default UI and agent |
| `readonly` | Cannot be modified after creation |
| `computed` | Derived value; not stored |
| `agent_hint=X` | Additional context for the agent |

### 10.5 Registry Compilation

At application startup, the framework:

1. Scans all registered Applications for types implementing `orjanda.Document`.
2. Parses struct tags and `DocMeta()` returns into `schema.CompiledDoc` structures.
3. Resolves Link and DynamicLink relationships into a relationship graph.
4. Validates schema consistency (e.g., Link targets exist, circular child tables are rejected).
5. Populates the in-memory Registry.
6. Generates agent tool definitions from the Registry (see §24).
7. Compares the Registry against the database and reports migration requirements.

---

## 11. Application and Module System

### 11.1 Application Structure

```
orjanda-app-hr/
├── go.mod                      # Go module (github.com/org/orjanda-app-hr)
├── app.go                      # Application descriptor
├── modules/
│   ├── recruitment/
│   │   ├── documents/
│   │   │   ├── job_opening.go
│   │   │   ├── job_applicant.go
│   │   │   └── interview.go
│   │   ├── hooks/
│   │   │   └── applicant_hooks.go
│   │   ├── workflows/
│   │   │   └── hiring_workflow.go
│   │   ├── api/
│   │   │   └── custom_methods.go   # Custom RPC methods
│   │   └── ui/
│   │       └── overrides.go        # UI customization declarations
│   └── leave/
│       ├── documents/
│       │   ├── leave_type.go
│       │   └── leave_request.go
│       ├── hooks/
│       │   └── leave_hooks.go
│       └── workflows/
│           └── leave_approval.go
├── fixtures/
│   └── seed_data.json
└── tests/
    ├── recruitment_test.go
    └── leave_test.go
```

### 11.2 Application Descriptor

```go
package hr

import "github.com/orjanda-framework/orjanda/app"

var App = app.Definition{
    Name:        "hr",
    Title:       "Human Resources",
    Version:     "0.1.0",
    Description: "HR management for Orjanda",
    Publisher:   "Orjanda Community",
    Modules: []app.Module{
        {Name: "recruitment", Title: "Recruitment"},
        {Name: "leave", Title: "Leave Management"},
    },
    Dependencies: []app.Dependency{
        {App: "core", MinVersion: "0.1.0"},
    },
}
```

### 11.3 Application Lifecycle

| Phase | Action |
|---|---|
| `Install` | Register documents, run initial migrations, load fixtures |
| `Upgrade` | Run pending migrations, execute upgrade hooks |
| `Uninstall` | Run teardown hooks, optionally drop tables |
| `Enable/Disable` | Toggle application without data loss |

### 11.4 Module vs. Application

**Decision:** Applications are Go modules (distributable packages). Modules are logical groupings within an Application.

**Rationale:** Go's module system provides versioning, dependency resolution, and distribution. Orjanda Applications are Go modules published to registries (GitHub, pkg.go.dev). This aligns with Go's ecosystem rather than inventing a custom package manager.

---

## 12. Backend Architecture

### 12.1 Core Components

```
orjanda/
├── cmd/
│   └── orjanda/              # CLI binary
├── app/                    # Application system
├── schema/                 # Schema types, annotations, registry
├── document/               # Document engine (CRUD, validation, hooks)
├── dal/                    # Data Access Layer
│   ├── query/              # Query builder
│   ├── dialect/            # Database dialect adapters
│   └── migrate/            # Migration engine
├── api/                    # HTTP API layer
│   ├── rest/               # REST handlers
│   ├── rpc/                # RPC method handlers
│   └── middleware/         # Auth, permissions, CORS, etc.
├── agent/                  # Agent Runtime
│   ├── runtime/            # Core agent loop
│   ├── tools/              # Tool generation and registry
│   ├── llm/                # LLM provider abstraction
│   ├── planner/            # Planning strategies
│   └── safety/             # Approval, guardrails
├── auth/                   # Authentication
├── perm/                   # Permission engine
├── workflow/               # Workflow state machine
├── event/                  # Event bus
├── search/                 # Search engine
├── audit/                  # Audit logging
├── cache/                  # Caching layer
├── background/             # Background job engine
├── ui/                     # UI serving and metadata API
└── server/                 # HTTP server assembly
```

### 12.2 Request Lifecycle

```
HTTP Request
  → Router (Chi)
  → CORS Middleware
  → Auth Middleware (extract identity from JWT/session)
  → Rate Limit Middleware
  → Permission Middleware (check document-level access)
  → Handler
    → Document Engine
      → Before-validation hooks
      → Schema validation
      → After-validation hooks
      → Before-save hooks
      → DAL operation (query builder → dialect → driver)
      → After-save hooks
      → Event emission
      → Audit log write
    → Response serialization
  → Response
```

### 12.3 HTTP Router

**Decision:** [Chi](https://github.com/go-chi/chi)

**Alternatives considered:**
- *Gin*: Faster benchmarks, but opinionated about JSON serialization and error handling. Its `gin.Context` leaks into business logic.
- *Echo*: Good feature set, but less Go-idiomatic (returns errors from handlers, custom context).
- *Standard `net/http`*: Viable in Go 1.22+ with path parameters, but lacks middleware composition ergonomics.
- *Chi*: 100% compatible with `net/http`. Composable middleware. Lightweight. No framework lock-in. The handler signature is `http.HandlerFunc`, which means any standard Go HTTP middleware works.

**Rationale:** Chi's `net/http` compatibility means the framework doesn't impose a custom handler interface. Developers can use any Go HTTP middleware. The composable router tree naturally maps to Orjanda's API structure.

### 12.4 Dependency Injection

**Decision:** Constructor injection with a central `orjanda.Site` container. No DI framework.

```go
type Site struct {
    Registry    *schema.Registry
    DB          dal.Database
    Permissions *perm.Engine
    EventBus    *event.Bus
    Agent       *agent.Runtime
    Cache       cache.Store
    // ...
}
```

**Rationale:** Go's interfaces and explicit construction are sufficient. DI frameworks (Wire, Dig) add complexity without proportional benefit for a framework of this scope. The `Site` object is the composition root, constructed at startup.

---

## 13. Database Architecture

### 13.1 Data Access Strategy

**Decision:** Custom query builder with dialect adapters. Not a traditional ORM.

**Alternatives considered:**
- *GORM*: Runtime reflection, hidden queries, type safety at runtime only. Unsuitable for a framework that needs predictable, auditable query generation.
- *Ent*: Excellent code generation and type safety, but imposes its own schema model. Orjanda needs to own the schema model entirely since Documents drive everything. Layering Orjanda's metadata on top of Ent's metadata creates redundancy and friction.
- *sqlc*: SQL-first is excellent for handwritten queries but incompatible with a metadata-driven system where queries are generated at runtime from dynamic schema definitions.
- *Custom query builder*: Orjanda's Document Engine generates queries from compiled schema metadata. A custom builder allows dialect-specific SQL generation, audit integration, and permission filtering to be injected at the query level.

**Rationale:** Orjanda's core value proposition is that schema metadata drives everything. The data access layer must be a direct consumer of the Registry, not a parallel schema system. A custom query builder — simpler than an ORM but more structured than raw SQL — gives the framework full control over query generation, dialect adaptation, and permission injection.

### 13.2 Query Builder Design

```go
// The query builder produces dialect-specific SQL from Document metadata.
results, err := site.DB.Query(ctx, dal.Select{
    DocType: "Employee",
    Fields:  []string{"FirstName", "LastName", "Department.Name"},
    Filters: dal.And(
        dal.Eq("Status", "Active"),
        dal.Gt("JoinDate", "2024-01-01"),
    ),
    OrderBy: "LastName",
    Limit:   20,
    Offset:  0,
})
```

The query builder:
1. Resolves field names against the Registry (validates fields exist and are accessible).
2. Applies permission filters automatically (e.g., `WHERE owner = ?` for role-restricted documents).
3. Translates Link traversals (e.g., `Department.Name`) into JOINs.
4. Delegates SQL generation to the dialect adapter.
5. Executes through the standard `database/sql` driver.

### 13.3 Dialect Adapters

| Dialect | Status | Notes |
|---|---|---|
| PostgreSQL | MVP | Full feature support, JSONB, full-text search |
| SQLite | MVP | Development/testing/small deployments |
| MySQL | Post-MVP | Community contribution target |

Each dialect adapter implements:
```go
type Dialect interface {
    CreateTable(doc schema.CompiledDoc) string
    AlterTable(diff schema.SchemaDiff) []string
    SelectSQL(q dal.Select) (string, []any)
    InsertSQL(doc string, fields map[string]any) (string, []any)
    UpdateSQL(doc string, id string, fields map[string]any) (string, []any)
    DeleteSQL(doc string, id string) (string, []any)
    FullTextSearch(doc string, query string, fields []string) (string, []any)
    Placeholder(n int) string  // $1 vs ?
}
```

### 13.4 Migration System

**Decision:** Atlas for declarative schema diffing, with Goose as the execution engine.

**Workflow:**
1. Developer modifies a Document struct.
2. `orjanda migrate diff` compares the compiled Registry against the current database state.
3. Orjanda uses Atlas's schema diff engine to compute the required ALTER statements.
4. The diff is written as a versioned migration file (SQL).
5. `orjanda migrate up` executes pending migrations via Goose.
6. Migrations are version-controlled alongside application code.

**Rationale:** Atlas eliminates the error-prone manual writing of ALTER TABLE statements. Goose provides a simple, reliable migration execution engine with support for both SQL and Go migration files (useful for data migrations that require business logic). The combination provides declarative convenience with explicit, reviewable migration files.

### 13.5 Schema Management

- **Forward-only migrations** in production. Rollbacks are new forward migrations.
- **Automatic diff detection** between Registry state and database state.
- **Data migration support** via Go migration files for complex transformations.
- **Multi-database awareness**: migration files are generated per-dialect when the application targets multiple databases.

### 13.6 Transactions

```go
err := site.DB.Transaction(ctx, func(tx dal.Tx) error {
    if err := tx.Insert(ctx, "LeaveRequest", leaveData); err != nil {
        return err
    }
    if err := tx.Update(ctx, "Employee", empID, updateData); err != nil {
        return err
    }
    return nil
})
```

All Document Engine write operations (create, update, delete, submit) are wrapped in transactions by default. Hook execution occurs within the same transaction, ensuring atomicity of business logic.

---

## 14. API Architecture

### 14.1 API Strategy

**Decision:** RESTful resource API for Documents + RPC-style API for custom methods. No GraphQL in MVP.

**Alternatives considered:**
- *REST only*: Natural fit for CRUD operations on Documents. Predictable URL structure. Well-understood caching semantics.
- *GraphQL*: Flexible querying, but adds significant complexity (schema generation, resolver implementation, N+1 prevention). Overkill for the primary use case and competes with the agent layer for "flexible query" use cases.
- *gRPC*: Excellent for service-to-service communication, but poor browser support without a proxy layer. Not appropriate as the primary external API.
- *REST + RPC hybrid*: REST for standard CRUD (predictable, cacheable), RPC for custom business operations (flexible, explicit). This matches Frappe's proven dual-API pattern.

**Rationale:** The agent runtime benefits from a predictable, uniform API. REST provides this for CRUD. Custom business operations (e.g., "approve leave request," "calculate payroll") are naturally RPC-style. GraphQL's flexibility is unnecessary when the agent can compose multiple REST calls and the UI has a metadata API for dynamic form/list generation.

### 14.2 REST API

| Operation | Method | URL | Body |
|---|---|---|---|
| List | `GET` | `/api/v1/document/{doctype}` | — |
| Read | `GET` | `/api/v1/document/{doctype}/{id}` | — |
| Create | `POST` | `/api/v1/document/{doctype}` | JSON fields |
| Update | `PATCH` | `/api/v1/document/{doctype}/{id}` | JSON fields |
| Delete | `DELETE` | `/api/v1/document/{doctype}/{id}` | — |
| Search | `GET` | `/api/v1/document/{doctype}?q=...&filters=...` | — |

**List query parameters:** `fields`, `filters` (JSON), `order_by`, `limit`, `offset`, `q` (full-text search).

### 14.3 RPC API

```
POST /api/v1/method/{app}.{module}.{method}
Content-Type: application/json

{ "employee_id": "EMP-001", "leave_type": "Annual" }
```

Custom methods are registered by Applications:
```go
func init() {
    api.RegisterMethod("hr.leave.get_balance", GetLeaveBalance,
        api.MethodOpts{
            AllowedRoles: []string{"HR Manager", "Employee"},
            HTTPMethod:   "GET",
        },
    )
}
```

### 14.4 Metadata API

The UI and agent consume schema metadata through a dedicated API:
```
GET /api/v1/meta/{doctype}          → Full schema for a Document
GET /api/v1/meta                    → List of all registered Documents
GET /api/v1/meta/{doctype}/links    → Relationship graph for a Document
```

### 14.5 Response Envelope

```json
{
    "data": { ... },
    "meta": {
        "total_count": 142,
        "limit": 20,
        "offset": 0
    },
    "error": null
}
```

All API responses use a consistent envelope. Error responses include machine-readable error codes and human-readable messages.

---

## 15. Authentication and Authorization

### 15.1 Authentication Strategy

**Decision:** JWT-based authentication with pluggable identity providers.

Orjanda provides a built-in authentication system for standalone deployments and an integration interface for external identity providers.

**Built-in:**
- Email/password authentication with bcrypt hashing.
- JWT access tokens (short-lived, 15 minutes) + refresh tokens (long-lived, 7 days).
- Session management with token rotation.

**External integration interface:**
```go
type AuthProvider interface {
    ValidateToken(ctx context.Context, token string) (*auth.Identity, error)
    GetUserInfo(ctx context.Context, token string) (*auth.UserInfo, error)
}
```

Implementations can wrap OAuth2/OIDC providers, LDAP, SAML, or custom SSO systems.

### 15.2 Identity Model

```go
type Identity struct {
    UserID    string
    Email     string
    FullName  string
    Roles     []string
    Tenant    string    // For multi-tenant deployments
    Source    string    // "local", "oauth:google", "ldap", etc.
}
```

The Identity is injected into `context.Context` by the Auth Middleware and is available to all downstream components — including the Agent Runtime.

---

## 16. Permissions Model

### 16.1 Permission Architecture

**Decision:** RBAC as the primary model with ABAC extensions for fine-grained rules.

**Alternatives considered:**
- *Pure RBAC*: Simple and sufficient for most business applications. Easy to reason about. Well-understood by developers.
- *Pure ABAC*: Maximum flexibility but difficult to audit, debug, and explain to business users.
- *Casbin*: Powerful but introduces a custom DSL that adds learning curve and makes permission logic opaque to the framework's introspection (the agent needs to understand permissions).
- *Custom RBAC + ABAC hybrid*: RBAC for document-level access (read, write, create, delete, submit, cancel). ABAC for contextual rules (owner-only access, department-scoped access, time-based restrictions). The permission model is introspectable by the Registry, which means the agent can query what operations are permitted.

**Rationale:** The permission system must be introspectable — the agent needs to know "can the current user update this document?" before attempting it. A custom hybrid model embedded in the Registry allows this introspection. Casbin's external policy engine would require a separate query path.

### 16.2 Permission Levels

| Level | Scope | Example |
|---|---|---|
| Document-level | CRUD + Submit/Cancel per DocType per Role | "HR Manager can Create Employee" |
| Field-level | Read/Write per field per Role | "Only HR Manager can see Salary" |
| Record-level | Owner match, department match, custom rules | "Employee can only read their own record" |
| Workflow-level | Transition permissions | "Only Department Head can approve leave" |

### 16.3 Permission Declaration

Permissions are declared in `DocMeta()` and evaluated at:
1. **API layer** — middleware checks document-level access before handler execution.
2. **Document Engine** — field-level and record-level checks during read/write.
3. **Agent Runtime** — permission check before tool execution (same engine, same rules).

### 16.4 Permission Evaluation

```go
type PermissionCheck struct {
    User     auth.Identity
    DocType  string
    Action   string   // "read", "write", "create", "delete", "submit", "cancel"
    RecordID string   // Optional: for record-level checks
    Fields   []string // Optional: for field-level checks
}

type PermissionResult struct {
    Allowed       bool
    AllowedFields []string // Fields the user can access
    DeniedFields  []string // Fields filtered from response
    Reason        string   // Human-readable denial reason
}
```

---

## 17. Admin UI Architecture

### 17.1 Frontend Framework

**Decision:** React with Tailwind CSS, served as a single-page application.

**Alternatives considered:**
- *Templ + HTMX (server-rendered)*: Eliminates the JS build pipeline and is excellent for simple CRUD. However, Orjanda's admin UI requires rich interactions: inline editing, drag-and-drop form builders (future), real-time agent chat, dynamic form rendering from metadata, and complex relationship visualization. These push beyond HTMX's sweet spot.
- *Vue*: Strong contender with good DX, but React's ecosystem depth (component libraries, hiring pool, community resources) is substantially larger.
- *Svelte*: Excellent performance and DX, but smaller ecosystem and component library availability.
- *React*: Largest ecosystem, most component libraries, best tooling support. The admin UI is metadata-driven (forms and lists are generated from schema metadata), which requires a component model that can dynamically compose UI from data — React's composition model excels here.

**Rationale:** The admin UI must dynamically generate forms, lists, and detail pages from schema metadata served by the backend. React's component model, combined with its ecosystem of data table, form, and chart libraries, provides the strongest foundation for this. Tailwind CSS is specified per requirements and provides the utility-first styling that metadata-driven UI generation benefits from (classes can be composed programmatically).

### 17.2 UI Architecture

```
orjanda-ui/
├── src/
│   ├── core/
│   │   ├── MetaProvider.tsx     # Fetches and caches schema metadata
│   │   ├── AuthProvider.tsx     # Authentication state
│   │   └── PermissionGuard.tsx  # Permission-aware rendering
│   ├── components/
│   │   ├── fields/              # Field renderers (Text, Select, Link, etc.)
│   │   ├── form/                # Auto-generated document form
│   │   ├── list/                # Auto-generated document list
│   │   ├── layout/              # Shell, sidebar, navigation
│   │   └── agent/               # Agent chat interface
│   ├── pages/
│   │   ├── DocFormPage.tsx      # Generic form page (driven by metadata)
│   │   ├── DocListPage.tsx      # Generic list page (driven by metadata)
│   │   └── DashboardPage.tsx    # Configurable dashboard
│   └── hooks/
│       ├── useDocument.ts       # CRUD operations for a document
│       ├── useMeta.ts           # Schema metadata access
│       └── useAgent.ts          # Agent interaction
```

### 17.3 Metadata-Driven Rendering

The UI does not contain hardcoded forms for each Document. Instead:

1. `DocFormPage` receives a `doctype` parameter from the URL.
2. It fetches the schema from `/api/v1/meta/{doctype}`.
3. The schema's field definitions are mapped to field renderer components.
4. The form is assembled dynamically: layout, field order, validation rules, and visibility conditions all derive from the schema.
5. Saving the form calls the standard REST API.

This means a new Document registered in the backend is immediately available in the UI without any frontend code changes.

### 17.4 Build and Serving

**Decision:** The React UI is built at compile time and embedded in the Go binary using `embed.FS`.

```go
//go:embed ui/dist/*
var uiAssets embed.FS
```

This produces a single deployable binary that serves both the API and the admin UI. For development, `npm run dev` runs the Vite dev server with API proxying to the Go backend.

---

## 18. UI Customization and Extension Model

### 18.1 Customization Layers

| Layer | Mechanism | Example |
|---|---|---|
| Field overrides | Schema annotations | Hide a field, change label, reorder |
| Custom field renderers | Component registry | Replace the default Date picker |
| Form layout | Layout descriptor in `DocMeta()` | Two-column layout, sections, tabs |
| Custom pages | Application-provided React routes | Custom dashboard, reports |
| Theme | Tailwind CSS theme configuration | Brand colors, typography |
| Component override | Named component registry | Replace the entire list view |

### 18.2 Component Registry

The admin UI uses a component registry pattern:

```typescript
// Default registration
ComponentRegistry.register('field:Date', DefaultDateField);

// Application override
ComponentRegistry.register('field:Date', CustomDateField);

// Document-specific override
ComponentRegistry.register('field:Date:Employee.JoinDate', HireDateField);
```

Resolution order: Document-specific → Application override → Default.

### 18.3 Custom Pages

Applications can register custom frontend routes:

```go
// In the Application's UI overrides
ui.RegisterPage(ui.Page{
    Path:      "/app/hr/org-chart",
    Title:     "Organization Chart",
    Component: "hr/OrgChart",  // Maps to a JS module in the app's UI bundle
    Icon:      "sitemap",
    Menu:      "HR",
})
```

The Application ships its custom React components as a separate bundle that is loaded on demand by the admin UI shell.

---

## 19. Workflow / Event System

### 19.1 Event Bus

The Event Bus is an in-process, synchronous-by-default pub/sub system.

**Events emitted by the Document Engine:**

| Event | Timing | Payload |
|---|---|---|
| `before_validate` | Before schema validation | Mutable document |
| `after_validate` | After validation passes | Mutable document |
| `before_insert` | Before first save | Mutable document |
| `after_insert` | After first save | Immutable document |
| `before_save` | Before any save | Mutable document |
| `after_save` | After any save | Immutable document |
| `before_submit` | Before submission | Mutable document |
| `after_submit` | After submission | Immutable document |
| `before_cancel` | Before cancellation | Mutable document |
| `after_cancel` | After cancellation | Immutable document |
| `before_delete` | Before deletion | Immutable document |
| `after_delete` | After deletion | Immutable document |
| `on_change:{field}` | When a specific field changes | Old value, new value |

### 19.2 Hook Registration

```go
func init() {
    event.On("Employee", "before_save", func(ctx context.Context, doc *document.Doc) error {
        if doc.Get("Status") == "Active" && doc.Get("Department") == "" {
            return errors.New("active employees must have a department")
        }
        return nil
    })
}
```

Hooks registered by Applications execute in dependency order (core hooks first, then application hooks).

### 19.3 Workflow State Machine

Workflows define state transitions for Submittable documents:

```go
var LeaveApproval = workflow.Definition{
    DocType: "LeaveRequest",
    States: []workflow.State{
        {Name: "Draft", Style: "gray"},
        {Name: "Pending Approval", Style: "yellow"},
        {Name: "Approved", Style: "green"},
        {Name: "Rejected", Style: "red"},
    },
    Transitions: []workflow.Transition{
        {From: "Draft", To: "Pending Approval", Action: "Submit",
            AllowedRoles: []string{"Employee"}},
        {From: "Pending Approval", To: "Approved", Action: "Approve",
            AllowedRoles: []string{"Department Head", "HR Manager"}},
        {From: "Pending Approval", To: "Rejected", Action: "Reject",
            AllowedRoles: []string{"Department Head", "HR Manager"}},
    },
    OnTransition: map[string]workflow.Handler{
        "Approved": func(ctx context.Context, doc *document.Doc) error {
            // Deduct leave balance
            return nil
        },
    },
}
```

The agent understands workflow states and available transitions. When a user asks "approve John's leave request," the agent checks: (1) the document is in "Pending Approval" state, (2) the user has the "Department Head" or "HR Manager" role, (3) executes the transition through the standard workflow engine.

---

## 20. Plugin / Extension System

### 20.1 Extension Strategy

**Decision:** In-process interface-based extensions compiled into the binary. No runtime plugin loading.

**Alternatives considered:**
- *Go native `plugin` package*: Requires identical Go toolchain versions, Linux-only in practice, no unloading, crash propagation. Unsuitable for production.
- *hashicorp/go-plugin (gRPC out-of-process)*: Excellent isolation but introduces serialization overhead and deployment complexity. Overkill for business application extensions that run in the same trust boundary.
- *WebAssembly (Wasm) plugins*: Interesting for sandboxed execution but immature Go→Wasm toolchain and significant performance overhead for the volume of calls a business application makes.
- *Interface-based, compile-time extensions*: Applications are Go packages that implement framework interfaces and are compiled into the final binary. No runtime loading, no version mismatch, no crash isolation concerns (same trust boundary). Extensions benefit from Go's type system.

**Rationale:** Orjanda Applications are Go packages. They are imported, compiled, and linked into the site binary. This is the simplest, most reliable, and most performant approach. It aligns with Go's philosophy of static compilation. The trade-off — requiring a rebuild to add/remove applications — is acceptable because business application deployments are not hot-swapped in production.

### 20.2 Extension Points

| Extension Point | Interface | Purpose |
|---|---|---|
| Document hooks | `event.Handler` | Business logic on document lifecycle |
| Custom API methods | `api.MethodHandler` | Additional RPC endpoints |
| Permission rules | `perm.Rule` | Custom permission evaluation |
| Field validators | `schema.Validator` | Custom field validation |
| Auth providers | `auth.Provider` | External authentication |
| LLM providers | `llm.Provider` | Additional LLM backends |
| Agent tools | `agent.Tool` | Custom agent capabilities |
| Search backends | `search.Backend` | Custom search engine |
| Cache backends | `cache.Store` | Custom cache implementation |
| Background jobs | `background.Job` | Custom async processing |
| UI pages | `ui.Page` | Custom frontend routes |

### 20.3 Application Composition

The site binary is assembled by importing Applications:

```go
package main

import (
    "github.com/orjanda-framework/orjanda"
    "github.com/orjanda-framework/orjanda-core"
    "github.com/myorg/orjanda-app-hr"
    "github.com/myorg/orjanda-app-inventory"
)

func main() {
    site := orjanda.NewSite(orjanda.Config{
        Database: orjanda.DatabaseConfig{...},
        LLM:     orjanda.LLMConfig{...},
    })

    site.Install(core.App)
    site.Install(hr.App)
    site.Install(inventory.App)

    site.Run()
}
```

---

## 21. CLI and Developer Experience

### 21.1 CLI Architecture

**Decision:** Cobra-based CLI (`orjanda`) as the primary developer interface.

```
orjanda init <app-name>          # Scaffold a new Orjanda application
orjanda new document <name>      # Generate a Document scaffold
orjanda new module <name>        # Generate a Module scaffold
orjanda serve                    # Start the development server
orjanda migrate diff             # Generate migration from schema changes
orjanda migrate up               # Apply pending migrations
orjanda migrate status           # Show migration status
orjanda console                  # Interactive REPL with site context
orjanda bench                    # Run the Orjanda site (production)
orjanda install <app>            # Install an application
orjanda uninstall <app>          # Uninstall an application
orjanda test                     # Run application tests
orjanda agent chat               # CLI-based agent chat for testing
orjanda registry list            # List all registered Documents
orjanda registry describe <doc>  # Show full schema for a Document
```

### 21.2 Developer Workflow

1. `orjanda init my-erp` — scaffolds a new site with `go.mod`, `main.go`, and core application.
2. `orjanda new document Employee` — generates `documents/employee.go` with a starter struct.
3. Developer edits the struct, adds fields, annotations, and `DocMeta()`.
4. `orjanda serve` — starts the dev server, compiles the Registry, auto-creates tables if needed.
5. Developer opens `http://localhost:8080` — admin UI shows Employee in the sidebar.
6. Developer opens `http://localhost:8080/agent` — agent can already query/create Employees.
7. `orjanda migrate diff` — generates migration SQL for production deployment.

### 21.3 Code Generation

The CLI generates idiomatic Go code scaffolds. Generated code is intended to be edited by the developer — it is a starting point, not a black box.

```
$ orjanda new document LeaveRequest --module=leave --submittable

Created: modules/leave/documents/leave_request.go
  → Document: LeaveRequest
  → Module: leave
  → Submittable: true
  → Generated DocMeta() with default permissions
  → Generated BaseDocument embedding
```

---

## 22. SDK Strategy

### 22.1 Go SDK (Primary)

The framework itself is the Go SDK. Applications import `github.com/orjanda-framework/orjanda` and use its packages directly.

Key packages exposed as public API:

| Package | Purpose |
|---|---|
| `orjanda/schema` | Document definition types, annotations, registry |
| `orjanda/document` | Document Engine operations |
| `orjanda/dal` | Data Access Layer, query builder |
| `orjanda/event` | Event bus, hook registration |
| `orjanda/workflow` | Workflow definitions |
| `orjanda/api` | Custom method registration |
| `orjanda/perm` | Permission rules |
| `orjanda/agent` | Custom agent tools |
| `orjanda/auth` | Authentication interfaces |
| `orjanda/app` | Application descriptor |
| `orjanda/testing` | Test utilities |

### 22.2 TypeScript SDK (Frontend)

A TypeScript SDK is auto-generated from the metadata API for frontend development:

```typescript
import { useOrjanda } from '@orjanda/react';

const { documents } = useOrjanda();

// Typed CRUD operations
const employees = await documents.Employee.list({
    filters: { status: 'Active' },
    fields: ['firstName', 'lastName', 'department'],
    limit: 20,
});

const emp = await documents.Employee.get('EMP-001');
await documents.Employee.update('EMP-001', { status: 'Inactive' });
```

### 22.3 REST Client SDK (Post-MVP)

Auto-generated REST client libraries for Python, JavaScript, and other languages, derived from the metadata API. This enables external integrations without manual API client construction.

---

## 23. Agent-Native Architecture

### 23.1 Core Principle: The Agent is an Internal Consumer

The Agent Runtime is not a separate service. It is an embedded component that consumes the same interfaces as the REST API and the Admin UI:

```
                  ┌─────────────┐
                  │  Registry   │
                  │  (Schemas,  │
                  │  Metadata)  │
                  └──────┬──────┘
                         │
          ┌──────────────┼──────────────┐
          │              │              │
    ┌─────▼─────┐  ┌─────▼─────┐  ┌─────▼─────┐
    │  REST API │  │  Admin UI │  │   Agent   │
    │  Handler  │  │  Metadata │  │  Runtime  │
    └─────┬─────┘  └─────┬─────┘  └─────┬─────┘
          │              │              │
          └──────────────┼──────────────┘
                         │
                  ┌──────▼──────┐
                  │  Document   │
                  │   Engine    │
                  │  (CRUD,     │
                  │  Validation,│
                  │  Hooks,     │
                  │  Permissions)│
                  └─────────────┘
```

All three consumers:
- Query the Registry for schema information.
- Execute operations through the Document Engine.
- Are subject to the Permission Engine.
- Trigger the Event Bus.
- Are recorded by the Audit Log.

The agent does not have a "back door." It calls `document.Create()`, `document.Update()`, `document.Get()` — the same functions the REST handlers call. This is the architectural guarantee that agent operations are consistent with human operations.

### 23.2 Agent Runtime Components

```
Agent Runtime
├── LLM Gateway         # Provider abstraction (OpenAI, Anthropic, etc.)
├── Tool Registry       # Auto-generated + custom tools
├── Planner             # Determines execution strategy
├── Executor            # Executes tool calls
├── Context Manager     # Conversation history, schema context
├── Safety Layer        # Approval gates, rate limits, scope guards
└── Session Manager     # Conversation state, multi-turn context
```

### 23.3 Agent Interaction Flow

```
1. User sends natural language request via Agent API or Chat UI.
2. Session Manager creates/resumes a conversation context.
3. Context Manager assembles:
   a. System prompt (framework identity, available capabilities)
   b. Schema context (relevant Document schemas, filtered by user permissions)
   c. Conversation history
   d. Available tools (auto-generated + custom, filtered by permissions)
4. Planner sends the assembled context to the LLM.
5. LLM returns a response:
   a. If text: return to user.
   b. If tool call: route to Executor.
6. Executor:
   a. Validates tool call arguments against schema.
   b. Checks permissions via Permission Engine.
   c. Checks Safety Layer (does this operation require approval?).
   d. If approved: executes via Document Engine.
   e. Returns result to Planner for next iteration.
7. Loop continues until LLM returns a final text response.
8. All operations are recorded in Audit Log.
```

### 23.4 Schema Context Optimization

A system with 50+ Document types cannot inject all schemas into every LLM call (context window exhaustion). The Agent Runtime uses a **two-phase context strategy:**

**Phase 1: Discovery tools.** The agent always has access to:
- `list_document_types()` — returns names and descriptions of all accessible Documents.
- `describe_document(doctype)` — returns the full schema for a specific Document.
- `list_relationships(doctype)` — returns the relationship graph for a Document.

**Phase 2: Operation tools.** Once the agent identifies the relevant Documents, it uses:
- `search_documents(doctype, query)` — full-text search.
- `list_documents(doctype, filters, fields)` — filtered listing.
- `get_document(doctype, id)` — read a specific record.
- `create_document(doctype, data)` — create a new record.
- `update_document(doctype, id, data)` — update a record.
- `delete_document(doctype, id)` — delete a record.
- `execute_action(doctype, id, action)` — workflow transitions.

This two-phase approach keeps the initial context small (only discovery tools + system prompt) while allowing the agent to explore the full schema on demand.

---

## 24. Automatic Agent Capability Generation

### 24.1 Tool Generation Pipeline

At Registry compilation time, the Agent Runtime generates tool definitions for each registered Document:

```
Registry.CompiledDocs
    → For each Document:
        → Check: is this Document hidden from agent? (annotation)
        → Generate: search tool (if Searchable)
        → Generate: list tool
        → Generate: read tool
        → Generate: create tool (if any role has Create permission)
        → Generate: update tool (if any role has Write permission)
        → Generate: delete tool (if any role has Delete permission)
        → Generate: action tools (for each workflow transition)
        → Generate: relationship traversal tools
    → For each registered RPC method:
        → Generate: method call tool
    → Compile tool definitions into JSON Schema format
```

### 24.2 Generated Tool Definition Example

For the `Employee` Document, the auto-generated `create_employee` tool:

```json
{
    "name": "create_employee",
    "description": "Create a new Employee record. An Employee represents a company employee record.",
    "parameters": {
        "type": "object",
        "properties": {
            "first_name": {
                "type": "string",
                "description": "First Name (required)"
            },
            "last_name": {
                "type": "string",
                "description": "Last Name (required)"
            },
            "email": {
                "type": "string",
                "format": "email",
                "description": "Email address (required, must be unique)"
            },
            "department": {
                "type": "string",
                "description": "Reference to a Department document"
            },
            "status": {
                "type": "string",
                "enum": ["Active", "Inactive", "On Leave"],
                "default": "Active",
                "description": "Employee status"
            },
            "join_date": {
                "type": "string",
                "format": "date",
                "description": "Join Date (required)"
            }
        },
        "required": ["first_name", "last_name", "email", "join_date"]
    }
}
```

Note: The `salary` field is excluded from the tool definition for users without the `hr_manager` role (field-level permission).

### 24.3 Custom Agent Tools

Developers can register additional agent tools beyond the auto-generated CRUD tools:

```go
agent.RegisterTool(agent.Tool{
    Name:        "calculate_leave_balance",
    Description: "Calculate the remaining leave balance for an employee",
    Parameters: agent.Params{
        {Name: "employee_id", Type: "string", Required: true,
            Description: "The Employee ID"},
        {Name: "leave_type", Type: "string", Required: true,
            Description: "Type of leave (Annual, Sick, etc.)"},
    },
    Handler: func(ctx context.Context, args map[string]any) (any, error) {
        // Business logic
        return calculateBalance(ctx, args["employee_id"].(string), args["leave_type"].(string))
    },
    AllowedRoles: []string{"HR Manager", "Employee"},
})
```

### 24.4 Agent Hints

Developers can provide additional context for the agent via annotations:

```go
type Employee struct {
    // ...
    EmployeeID string `oj:"unique,agent_hint=This is the primary identifier used in conversations"`
    Department schema.Link `oj:"link=Department,agent_hint=Always confirm department exists before assigning"`
}
```

---

## 25. Agent Permission and Security Model

### 25.1 Principle: No Privilege Escalation

The agent operates with the **authenticated user's identity and permissions**. The agent cannot access data or perform operations that the user cannot.

```
User authenticates → Identity injected into context
    → Agent receives the same Identity
    → Agent's tool calls pass through Permission Engine
    → Permission Engine evaluates against the user's roles
    → Denied operations return error to agent (not to LLM directly)
```

### 25.2 Permission Flow for Agent Tool Execution

```
Agent calls create_employee(data)
    → Executor extracts Identity from context
    → Permission Engine: Can this user Create Employee? 
        → YES: proceed
        → NO: return PermissionDenied error to agent
    → Permission Engine: filter fields by user's field-level access
    → Document Engine: validate, save
    → Audit Log: record operation with agent=true flag
```

### 25.3 Agent-Specific Security Controls

| Control | Description |
|---|---|
| Rate limiting | Per-user, per-session limits on agent operations |
| Scope restriction | Admin can restrict agent to read-only for specific roles |
| Audit flag | All agent operations are flagged as `via_agent=true` in audit log |
| Token budget | Per-session LLM token budget to prevent runaway costs |
| Tool allowlist | Admin can restrict which tools are available to the agent |
| Sensitive field masking | Fields marked `agent_hidden` are excluded from agent context |

### 25.4 Agent Identity

The agent does not have its own identity. It always operates as the authenticated user. The audit log records both the user identity and the fact that the operation was agent-assisted:

```json
{
    "user": "jane@example.com",
    "doctype": "LeaveRequest",
    "action": "create",
    "via_agent": true,
    "agent_session": "session-abc123",
    "agent_prompt": "Create a leave request for next Monday",
    "timestamp": "2026-08-09T10:30:00Z"
}
```

---

## 26. LLM Provider Abstraction

### 26.1 Provider Interface

```go
type Provider interface {
    ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
    SupportsToolCalling() bool
    SupportsStructuredOutput() bool
    ModelInfo() ModelInfo
}

type ChatRequest struct {
    Model       string
    Messages    []Message
    Tools       []ToolDefinition
    Temperature float64
    MaxTokens   int
    // Provider-specific options via functional options pattern
}

type ChatResponse struct {
    Content    string
    ToolCalls  []ToolCall
    Usage      TokenUsage
    FinishReason string
}
```

### 26.2 Built-in Providers

| Provider | MVP | Notes |
|---|---|---|
| OpenAI | Yes | GPT-4o, GPT-4o-mini |
| Anthropic | Yes | Claude Sonnet, Claude Haiku |
| Ollama (local) | Post-MVP | For self-hosted / air-gapped deployments |
| Google (Gemini) | Post-MVP | Community contribution |

### 26.3 Provider Configuration

```yaml
# orjanda.yaml
llm:
  default_provider: openai
  providers:
    openai:
      api_key: ${OPENAI_API_KEY}
      model: gpt-4o
      max_tokens: 4096
    anthropic:
      api_key: ${ANTHROPIC_API_KEY}
      model: claude-sonnet-4-20250514
      max_tokens: 4096
  fallback:
    - openai
    - anthropic
  routing:
    simple_queries: gpt-4o-mini    # Cost optimization
    complex_reasoning: gpt-4o      # Quality optimization
```

### 26.4 Provider Resilience

- **Automatic failover:** If the primary provider returns a 5xx error, retry with the fallback provider.
- **Circuit breaker:** After N consecutive failures, stop sending to a provider for a cooldown period.
- **Token budget tracking:** Track per-session and per-user token usage across providers.

---

## 27. Agent Planning and Execution Model

### 27.1 Planning Strategy

**Decision:** ReAct (Reason + Act) as the primary planning pattern, with structured Plan-and-Execute for multi-step operations.

**Alternatives considered:**
- *Pure ReAct*: Flexible, traceable, works well for open-ended queries. Can be slow and expensive for multi-step tasks due to repeated LLM calls.
- *Pure Plan-and-Execute*: Generates a plan upfront, then executes it. Faster for structured tasks but brittle if the plan is incorrect (no mid-course correction).
- *Multi-agent*: Specialized agents for different domains. Adds architectural complexity without clear benefit at this stage.
- *Hybrid ReAct + Plan-and-Execute*: Use ReAct for single-step and exploratory queries. When the agent detects a multi-step task, it generates a plan, confirms with the user, then executes with per-step validation. This provides both flexibility and efficiency.

**Rationale:** Business applications have a mix of simple queries ("how many employees are in Engineering?") and multi-step operations ("create a leave request for John, check his balance, and notify his manager"). The hybrid approach handles both efficiently.

### 27.2 Execution Loop

```
func (r *Runtime) Execute(ctx context.Context, userMessage string) (*Response, error) {
    session := r.SessionManager.GetOrCreate(ctx)
    
    for iteration := 0; iteration < r.MaxIterations; iteration++ {
        // Assemble context
        prompt := r.ContextManager.Build(ctx, session, userMessage)
        
        // Call LLM
        response, err := r.LLM.ChatCompletion(ctx, prompt)
        if err != nil {
            return nil, err
        }
        
        // If text response, return to user
        if len(response.ToolCalls) == 0 {
            session.AddAssistantMessage(response.Content)
            return &Response{Text: response.Content}, nil
        }
        
        // Execute tool calls
        for _, call := range response.ToolCalls {
            result, err := r.Executor.Execute(ctx, call)
            session.AddToolResult(call, result, err)
        }
        
        // Continue loop with tool results in context
    }
    
    return nil, ErrMaxIterationsExceeded
}
```

### 27.3 Deterministic Operations

For operations where LLM variability is undesirable, Orjanda uses **structured output** to force the LLM to produce a specific JSON schema:

```go
// Instead of free-form tool calls, the agent is instructed to produce:
{
    "operation": "create",
    "doctype": "LeaveRequest",
    "data": {
        "employee": "EMP-001",
        "leave_type": "Annual",
        "from_date": "2026-08-15",
        "to_date": "2026-08-16"
    }
}
```

The framework validates this structured output against the Document schema before execution, ensuring type correctness regardless of LLM output quality.

---

## 28. Human Approval / Safety Model

### 28.1 Approval Gates

Certain operations require explicit user confirmation before the agent executes them:

| Operation Category | Default Policy | Configurable |
|---|---|---|
| Read operations | Auto-approve | Yes |
| Search operations | Auto-approve | Yes |
| Create operations | Require approval | Yes |
| Update operations | Require approval | Yes |
| Delete operations | Always require approval | Yes |
| Workflow transitions | Require approval | Yes |
| Bulk operations (>5 records) | Always require approval | No |
| Custom methods (side effects) | Require approval | Yes |

### 28.2 Approval Flow

```
Agent determines action: "Create LeaveRequest for EMP-001"
    → Safety Layer checks approval policy
    → If approval required:
        → Return to user: "I'd like to create a leave request with these details:
            Employee: John Smith (EMP-001)
            Type: Annual Leave
            Dates: Aug 15-16, 2026
            
            Shall I proceed? [Approve / Deny / Modify]"
        → User responds
        → If approved: execute
        → If denied: cancel with acknowledgment
        → If modify: re-plan with modifications
```

### 28.3 Configurable Safety Policies

```go
agent.SetSafetyPolicy(agent.SafetyPolicy{
    AutoApprove: []string{"read", "search", "list"},
    RequireApproval: []string{"create", "update", "submit"},
    AlwaysRequireApproval: []string{"delete", "cancel"},
    MaxBulkOperations: 5,
    RequireApprovalForRoles: map[string][]string{
        "Intern": {"create", "update"},   // Interns need approval for everything
    },
})
```

---

## 29. Auditability and Observability

### 29.1 Audit Log

Every Document operation is recorded in an immutable audit log:

```go
type AuditEntry struct {
    ID          string
    Timestamp   time.Time
    UserID      string
    DocType     string
    DocID       string
    Action      string        // "create", "update", "delete", "submit", etc.
    Changes     []FieldChange // Old value → new value for each changed field
    ViaAgent    bool
    AgentSession string
    AgentPrompt  string       // The user's original request (if agent-initiated)
    IPAddress   string
    UserAgent   string
}

type FieldChange struct {
    Field    string
    OldValue any
    NewValue any
}
```

### 29.2 Agent Observability

Agent sessions are fully traceable:

| Metric | Purpose |
|---|---|
| Token usage per session | Cost tracking |
| Tool call count per session | Efficiency monitoring |
| Permission denials | Security monitoring |
| Approval request rate | Safety policy tuning |
| LLM latency per call | Performance monitoring |
| Error rate per provider | Reliability tracking |

### 29.3 Structured Logging

Orjanda uses structured logging (`slog`) throughout:

```go
slog.Info("document.created",
    "doctype", "Employee",
    "id", emp.ID,
    "user", identity.UserID,
    "via_agent", true,
    "duration_ms", elapsed.Milliseconds(),
)
```

### 29.4 OpenTelemetry Integration

**Post-MVP:** Orjanda will integrate with OpenTelemetry for distributed tracing and metrics export. In MVP, structured logging and the audit log provide sufficient observability.

---

## 30. Multi-Tenancy Considerations

### 30.1 Strategy

**Decision:** Row-level tenant isolation using a `tenant_id` column, not schema-per-tenant.

**Rationale:** Schema-per-tenant (separate databases or schemas per tenant) provides stronger isolation but creates migration complexity (every migration must run N times) and connection pool pressure. Row-level isolation with consistent `tenant_id` filtering is simpler, more scalable for moderate tenant counts, and aligns with the single-binary deployment model.

### 30.2 Implementation

- Every Document table includes a `tenant_id` column (automatically added by BaseDocument when multi-tenancy is enabled).
- The DAL query builder automatically injects `WHERE tenant_id = ?` on every query.
- The `tenant_id` is extracted from the `auth.Identity` in the request context.
- Cross-tenant queries are architecturally impossible without explicit bypass (admin-only).

### 30.3 MVP Scope

Multi-tenancy is **not in MVP scope**. The architecture accommodates it (the Identity model includes `Tenant`, the DAL can inject tenant filters), but implementation and testing are deferred to post-MVP.

---

## 31. Internationalization / Localization

### 31.1 Strategy

- **UI labels:** Translatable via a message catalog system. Schema `label` annotations serve as translation keys.
- **Data:** Stored in the original language. Multi-language data fields are a post-MVP feature.
- **Agent:** The agent communicates in the language of the user's request (handled by the LLM naturally). System prompts are in English; the LLM handles translation.
- **Date/number formatting:** Locale-aware formatting in the UI, driven by the user's locale preference.

### 31.2 MVP Scope

MVP ships with English only. The architecture supports i18n (label annotations, message catalog interface), but translated message catalogs are not included.

---

## 32. Testing Strategy

### 32.1 Framework Testing

| Layer | Strategy | Tools |
|---|---|---|
| Unit tests | Test individual packages in isolation | `testing` + `testify` |
| Integration tests | Test Document Engine + DAL against real databases | `testcontainers-go` (PostgreSQL), in-memory SQLite |
| API tests | Test HTTP endpoints end-to-end | `net/http/httptest` |
| Agent tests | Test tool generation, execution, and permission enforcement | Mock LLM provider |
| Migration tests | Verify schema diffs produce correct SQL | Snapshot testing |

### 32.2 Application Testing

Orjanda provides a `orjanda/testing` package with utilities:

```go
func TestLeaveRequestCreation(t *testing.T) {
    site := ntest.NewTestSite(t, ntest.WithApps(hr.App))
    
    // Create test user with specific roles
    user := site.CreateUser(t, "jane@test.com", "HR Manager")
    ctx := site.WithUser(user)
    
    // Test document creation
    doc, err := site.Document.Create(ctx, "LeaveRequest", map[string]any{
        "Employee":  "EMP-001",
        "LeaveType": "Annual",
        "FromDate":  "2026-08-15",
        "ToDate":    "2026-08-16",
    })
    require.NoError(t, err)
    assert.Equal(t, "Draft", doc.Get("WorkflowState"))
    
    // Test permission enforcement
    intern := site.CreateUser(t, "bob@test.com", "Intern")
    internCtx := site.WithUser(intern)
    _, err = site.Document.Delete(internCtx, "LeaveRequest", doc.ID)
    assert.ErrorIs(t, err, perm.ErrPermissionDenied)
}
```

### 32.3 Agent Testing

```go
func TestAgentCanSearchEmployees(t *testing.T) {
    site := ntest.NewTestSite(t, ntest.WithApps(hr.App))
    site.SeedFixtures(t, "testdata/employees.json")
    
    // Use a mock LLM that returns predetermined tool calls
    mock := ntest.MockLLM(t,
        ntest.ToolCall("search_documents", map[string]any{
            "doctype": "Employee",
            "query":   "engineering",
        }),
        ntest.TextResponse("Found 5 employees in Engineering."),
    )
    
    user := site.CreateUser(t, "jane@test.com", "HR Manager")
    ctx := site.WithUser(user)
    
    resp, err := site.Agent.Execute(ctx, "How many employees are in Engineering?",
        agent.WithProvider(mock))
    require.NoError(t, err)
    assert.Contains(t, resp.Text, "Engineering")
}
```

---

## 33. Performance and Scalability

### 33.1 Performance Targets (MVP)

| Metric | Target |
|---|---|
| API response (simple CRUD) | < 50ms p95 |
| Document list (1000 records, paginated) | < 100ms p95 |
| Registry compilation (100 Documents) | < 2 seconds at startup |
| Agent response (single tool call) | < 3 seconds (dominated by LLM latency) |
| Concurrent users | 100+ per instance |

### 33.2 Caching Strategy

- **Registry:** Compiled in-memory at startup. Read-only after compilation. Zero-cost access.
- **Permission evaluations:** Cached per-user per-request (permission checks for the same user/doctype/action within a single request are evaluated once).
- **Document metadata:** Cached in-memory. The metadata API serves from cache.
- **Query results:** No automatic query caching in MVP. Applications can use the cache interface for custom caching.

### 33.3 Scalability Model

Orjanda is a stateless application server (excluding agent sessions). Horizontal scaling is achieved by running multiple instances behind a load balancer:

```
Load Balancer
    ├── Orjanda Instance 1
    ├── Orjanda Instance 2
    └── Orjanda Instance 3
         ↓
    Shared Database (PostgreSQL)
```

Agent sessions are stored in-memory per instance in MVP. Sticky sessions (via load balancer) ensure conversation continuity. Post-MVP, agent sessions can be externalized to Redis for stateless scaling.

---

## 34. Extensibility Strategy

### 34.1 Extension Hierarchy

```
Orjanda Core Framework
    └── Core Application (orjanda-core)
        ├── User, Role, Permission Documents
        ├── System Settings
        ├── Audit Log
        └── Agent Configuration
    └── Community / Third-Party Applications
        ├── orjanda-app-hr
        ├── orjanda-app-inventory
        ├── orjanda-app-accounting
        └── orjanda-app-custom
```

### 34.2 Extension Principles

1. **Applications cannot modify core framework behavior**, only extend it through defined interfaces.
2. **Applications can depend on other applications** (e.g., HR depends on Core).
3. **Applications can hook into other applications' Documents** (e.g., Payroll can add hooks to Employee).
4. **Applications can add fields to other applications' Documents** via schema extension (post-MVP).
5. **Conflicts are resolved by dependency order** (later-installed application wins for hooks on the same event).

---

## 35. Ecosystem Strategy

### 35.1 Distribution

- Applications are standard Go modules, distributed via Git repositories.
- No custom package registry in MVP. `go get` is the installation mechanism.
- Post-MVP: a curated application directory (website) listing community applications with compatibility information.

### 35.2 Community Model

- **Core framework:** Maintained by the Orjanda team. Strict backward compatibility.
- **Official applications:** A small set of reference applications (e.g., `orjanda-app-hr-example`) maintained by the Orjanda team to demonstrate best practices.
- **Community applications:** Third-party applications following Orjanda conventions. Quality and compatibility are the author's responsibility.

### 35.3 MCP Compatibility (Post-MVP)

The Agent Runtime should expose an MCP (Model Context Protocol) server interface, allowing external MCP clients (IDE agents, chat interfaces) to discover and use Orjanda's auto-generated tools. This positions Orjanda as an MCP-compliant tool provider, enabling integration with the broader AI agent ecosystem.

---

## 36. Example Application Architecture

### 36.1 Minimal HR Application

This example demonstrates how a developer would build a basic HR module on Orjanda:

**Documents:**
- `Department` — name, head (Link to Employee), description
- `Employee` — first name, last name, email, department (Link), status, join date, salary
- `LeaveType` — name, max days per year, is paid
- `LeaveRequest` — employee (Link), leave type (Link), from date, to date, status, reason

**Relationships:**
```
Department ←──── Employee (many employees per department)
Employee ──────→ LeaveRequest (one employee, many leave requests)
LeaveType ─────→ LeaveRequest (one leave type per request)
```

**Workflow:** LeaveRequest follows: Draft → Pending Approval → Approved/Rejected

**Total developer code:** ~200 lines of Go (4 Document structs + DocMeta + 1 workflow definition + 2 hooks for business logic).

**What Orjanda provides automatically:**
- 4 database tables with proper indexes and foreign keys
- Full REST API for all 4 Documents
- Admin UI with forms and lists for all 4 Documents
- Search across all searchable fields
- Permission enforcement per role
- Audit log for all operations
- Agent tools for querying, creating, and managing all 4 Documents
- Workflow transitions with role-based gating

---

## 37. Example Developer Workflow

```
Day 1: Project Setup
$ orjanda init my-hr-system
$ cd my-hr-system
$ orjanda new document Department --module=org
$ orjanda new document Employee --module=org
$ orjanda new document LeaveType --module=leave
$ orjanda new document LeaveRequest --module=leave --submittable

Day 1: Define Schemas (edit generated files, add fields + DocMeta)
# ~30 minutes of editing Go structs

Day 1: First Run
$ orjanda serve
# → Registry compiles 4 Documents
# → Tables created in SQLite (dev mode)
# → Admin UI available at localhost:8080
# → Agent available at localhost:8080/agent

Day 1: Test
# Open browser → create a Department → create an Employee
# Open agent chat → "list all employees" → works
# Open agent chat → "create a leave request for John, annual leave, Aug 15-16" → works

Day 2: Add Business Logic
# Add hooks: validate leave balance, notify manager on submission
# Add workflow: leave approval state machine
# Add custom agent tool: calculate_leave_balance

Day 2: Prepare for Production
$ orjanda migrate diff    # Generate migration SQL
$ orjanda test           # Run tests
# Deploy binary + apply migrations
```

---

## 38. Example Agent Workflow

### 38.1 Simple Query

```
User: "How many employees are in the Engineering department?"

Agent internally:
  1. Tool call: list_document_types() → discovers Employee, Department, etc.
  2. Tool call: search_documents(doctype="Employee", 
       filters={"department.name": "Engineering"}) 
     → returns 12 results
  3. Response: "There are 12 employees in the Engineering department."
```

### 38.2 Multi-Step Operation with Approval

```
User: "Submit a leave request for me. Annual leave, next Monday to Wednesday."

Agent internally:
  1. Tool call: get_document(doctype="Employee", 
       filters={"email": "jane@example.com"}) → gets user's employee record
  2. Tool call: calculate_leave_balance(employee_id="EMP-042", 
       leave_type="Annual") → 15 days remaining
  3. Determines: create + submit operation → requires approval
  4. Response: "I'd like to create a leave request with these details:
       - Type: Annual Leave
       - From: Monday, Aug 11, 2026
       - To: Wednesday, Aug 13, 2026
       - Duration: 3 days
       - Remaining balance after: 12 days
       
       Shall I proceed?"
  
User: "Yes"

Agent internally:
  5. Tool call: create_document(doctype="LeaveRequest", data={...})
  6. Tool call: execute_action(doctype="LeaveRequest", id="LR-099", 
       action="Submit")
  7. Response: "Done! Your leave request LR-099 has been submitted for 
       approval. Your manager will be notified."
```

### 38.3 Permission Denial

```
User: "Delete all employees in the Intern department"

Agent internally:
  1. Safety Layer: bulk delete → always requires approval
  2. Response: "I can't perform bulk delete operations. Deletion of 
       employee records requires individual review. Would you like me 
       to list the employees in the Intern department instead?"
```

---

## 39. Competitive / Architectural Comparison

### 39.1 Orjanda vs. Frappe

| Dimension | Frappe | Orjanda |
|---|---|---|
| Language | Python | Go |
| Schema Definition | JSON metadata (stored in DB) | Go structs (compiled at startup) |
| Type Safety | Runtime | Compile-time |
| Agent Support | None (requires external integration) | First-class, embedded |
| Performance | Moderate (Python) | High (Go, compiled binary) |
| Deployment | Complex (bench, Redis, MariaDB, nginx) | Single binary + database |
| Frontend | Custom JS framework (Frappe UI) | React + Tailwind CSS |
| Extensibility | Hot-reload apps at runtime | Compile-time app composition |
| Schema Changes | UI-editable, no code required | Code change + migration |
| Ecosystem Maturity | Very mature (ERPNext, etc.) | New, must build |
| Developer Pool | Large (Python/JS) | Growing (Go) |
| Multi-database | MariaDB only | PostgreSQL, SQLite (extensible) |

**Key trade-off:** Frappe's runtime schema editing enables non-developer customization. Orjanda sacrifices this for compile-time safety and performance. This is a deliberate choice: Orjanda targets developer teams, not no-code users.

### 39.2 Orjanda vs. Django (+ Django Admin)

| Dimension | Django | Orjanda |
|---|---|---|
| Language | Python | Go |
| Admin UI | Django Admin (auto-generated) | Orjanda Admin (auto-generated, React) |
| ORM | Django ORM | Custom query builder |
| Agent Support | None (requires external integration) | First-class, embedded |
| API Generation | Django REST Framework (separate package) | Built-in REST + RPC |
| Performance | Moderate | High |
| Type Safety | Moderate (Python typing) | Strong (Go) |
| Ecosystem | Massive | New |
| Real-time | Channels (add-on) | WebSocket for agent chat |

**Key insight:** Django Admin is the closest existing analog to Orjanda's auto-generated UI. Orjanda extends this concept by adding auto-generated agent capabilities on top.

### 39.3 Orjanda vs. Building from Scratch (Go + Gin + GORM + React)

| Dimension | Custom Stack | Orjanda |
|---|---|---|
| Time to first CRUD | Hours | Minutes |
| Agent integration | Weeks per entity | Automatic |
| Permission system | Build from scratch | Built-in |
| Schema consistency | Manual synchronization | Single source of truth |
| Admin UI | Build from scratch | Auto-generated |
| Audit log | Build from scratch | Automatic |
| Migration system | Configure separately | Integrated |

---

## 40. Key Technical Risks

| ID | Risk | Impact | Mitigation |
|---|---|---|---|
| R1 | Go reflection limitations make struct tag parsing brittle | Schema compilation bugs | Extensive test suite for tag parsing; consider code generation alternative if reflection proves insufficient |
| R2 | Custom query builder has dialect bugs | Data corruption or loss | Comprehensive SQL generation tests per dialect; snapshot testing of generated SQL |
| R3 | LLM tool calling produces invalid arguments | Agent execution failures | Schema validation layer between LLM output and Document Engine; structured output enforcement |
| R4 | Agent context window exhaustion with many Documents | Agent becomes ineffective | Two-phase context strategy (§23.4); lazy schema loading; context summarization |
| R5 | Permission bypass through agent | Security vulnerability | Unified permission engine; no separate agent permission path; security audit |
| R6 | React admin UI bundle size grows with metadata-driven components | Slow initial load | Code splitting per page; lazy loading; component tree-shaking |
| R7 | Single-binary embedding limits frontend development velocity | Slow UI iteration cycle | Vite dev server proxy during development; only embed for production builds |
| R8 | Compile-time app composition prevents runtime app management | Ops flexibility reduced | Accept this trade-off; provide CLI tooling to streamline rebuild/redeploy |

---

## 41. Important Trade-offs

### 41.1 Code-First vs. Runtime Metadata

**Chose:** Code-first (Go structs).
**Gave up:** Runtime schema editing, non-developer customization.
**Gained:** Compile-time safety, IDE support, version control, code review.
**Justification:** The target audience is developers. Type safety in Go is too valuable to sacrifice.

### 41.2 Embedded Agent vs. External Agent Service

**Chose:** Embedded (in-process).
**Gave up:** Independent scaling of agent, language flexibility for agent code.
**Gained:** Zero-latency access to Registry, Permission Engine, and Document Engine. No serialization overhead. Single deployment unit.
**Justification:** The agent must be tightly coupled to the application metadata. An external service would require synchronizing schemas, permissions, and validation rules — recreating the O(n) problem Orjanda aims to eliminate.

### 41.3 Custom Query Builder vs. Established ORM

**Chose:** Custom query builder.
**Gave up:** Ecosystem maturity, community support, battle-tested edge cases.
**Gained:** Full control over query generation from metadata, permission injection, dialect adaptation.
**Justification:** No existing Go ORM is designed to generate queries from runtime metadata. The custom builder is scoped to Document operations (not general-purpose SQL), which limits complexity.

### 41.4 React SPA vs. Server-Rendered (Templ + HTMX)

**Chose:** React SPA.
**Gave up:** Simpler deployment (no JS build), Go-only stack.
**Gained:** Rich interactive UI, agent chat interface, metadata-driven dynamic form rendering, massive component ecosystem.
**Justification:** The admin UI requirements (dynamic forms from metadata, real-time agent chat, interactive relationship visualization) exceed what HTMX delivers ergonomically.

### 41.5 Compile-Time Plugins vs. Runtime Plugin Loading

**Chose:** Compile-time (Go package imports).
**Gave up:** Hot-swappable applications, runtime extension loading.
**Gained:** Type safety, no version mismatch issues, no crash isolation concerns, simpler deployment.
**Justification:** Business applications are not plugin marketplaces. Application changes go through code review and deployment pipelines.

---

## 42. Recommended Technology Choices

| Component | Technology | Rationale |
|---|---|---|
| **Language** | Go 1.22+ | Requirement; excellent performance, single binary, strong typing |
| **HTTP Router** | Chi | net/http compatible, composable middleware, lightweight |
| **CLI** | Cobra | Industry standard for Go CLIs, subcommand support |
| **Database Access** | Custom query builder + `database/sql` | Metadata-driven query generation requirement |
| **Database Drivers** | `pgx` (PostgreSQL), `modernc.org/sqlite` (SQLite) | Pure Go, no CGo dependency for SQLite |
| **Migration Diffing** | Atlas (as library) | Declarative schema diffing |
| **Migration Execution** | Goose | Simple, reliable, supports Go migration files |
| **Authentication** | JWT (`golang-jwt/jwt`) | Standard, stateless, well-supported |
| **Password Hashing** | bcrypt (`golang.org/x/crypto/bcrypt`) | Industry standard |
| **ID Generation** | ULID (`oklog/ulid`) | Sortable, globally unique, URL-safe |
| **Logging** | `log/slog` (stdlib) | Structured logging, no external dependency |
| **Configuration** | Viper | File + env var config, YAML support |
| **Testing** | `testify` + `testcontainers-go` | Assertions + real database testing |
| **Frontend** | React 19+ | Largest ecosystem, dynamic component composition |
| **Frontend Build** | Vite | Fast development server, optimized builds |
| **CSS** | Tailwind CSS | Utility-first, metadata-driven class composition |
| **UI Embedding** | `embed.FS` (stdlib) | Single binary deployment |
| **LLM Client** | Custom (per-provider HTTP clients) | Avoid leaky abstractions from third-party SDKs |
| **Decimal Math** | `shopspring/decimal` | Precise currency calculations |
| **Validation** | Custom (schema-driven) | Integrated with Document Engine |

---

## 43. Future Evolution

### 43.1 Post-MVP Roadmap (Indicative)

| Phase | Features |
|---|---|
| **v0.2** | MySQL dialect, multi-tenancy, OpenTelemetry, background job scheduling |
| **v0.3** | MCP server interface, Ollama/local LLM provider, visual workflow designer |
| **v0.4** | Schema extension (add fields to other apps' Documents), field-level i18n |
| **v0.5** | Report builder, dashboard widgets, chart generation |
| **v0.6** | Print formats, PDF generation, email integration |
| **v1.0** | Stable public API, backward compatibility guarantee, first official application |

### 43.2 Long-Term Vision

- **Orjanda Marketplace:** A curated directory of community applications.
- **Orjanda Cloud:** Managed hosting for Orjanda-based applications (commercial).
- **Multi-agent workflows:** Specialized agents for different business domains within the same site.
- **Agent learning:** Episodic memory — agents learn from past interactions to improve accuracy.
- **AI-assisted development:** The agent can help developers define new Documents and business logic through conversation.

---

## 44. MVP Scope

### 44.1 MVP Objective

Prove the central thesis: **A developer can define business entities in Orjanda, and the resulting application is automatically operable by an AI agent without manually creating individual agent integrations per entity.**

### 44.2 MVP Feature Set

| Component | MVP Scope |
|---|---|
| **Schema System** | Go struct definitions, struct tags, BaseDocument, child tables, Links |
| **Registry** | Compile Go structs into in-memory metadata at startup |
| **Document Engine** | Create, Read, Update, Delete, List, Search |
| **Database** | PostgreSQL + SQLite, custom query builder, basic dialect adapters |
| **Migrations** | `orjanda migrate diff` + `orjanda migrate up` |
| **REST API** | Full CRUD + List + Search for all Documents |
| **RPC API** | Custom method registration and invocation |
| **Authentication** | JWT-based, email/password, role assignment |
| **Permissions** | Document-level RBAC (read/write/create/delete per role) |
| **Admin UI** | React + Tailwind: auto-generated form + list pages for all Documents |
| **Agent Runtime** | Embedded, ReAct loop, tool calling |
| **Auto Tool Generation** | CRUD tools generated per Document from Registry |
| **LLM Providers** | OpenAI + Anthropic |
| **Safety** | Approval gates for write operations |
| **Audit Log** | All operations logged with agent flag |
| **Hooks** | Document lifecycle hooks (before_save, after_insert, etc.) |
| **Workflows** | Basic state machine with role-gated transitions |
| **CLI** | `init`, `serve`, `new document`, `migrate diff/up`, `agent chat` |
| **Agent Chat UI** | Basic chat interface in the admin panel |

### 44.3 MVP Non-Scope

| Feature | Deferred To |
|---|---|
| Field-level permissions | v0.2 |
| Multi-tenancy | v0.2 |
| MySQL support | v0.2 |
| OpenTelemetry | v0.2 |
| Background jobs | v0.2 |
| MCP server | v0.3 |
| Schema extension (cross-app) | v0.4 |
| Report builder | v0.5 |
| Print formats | v0.6 |
| i18n / l10n | v0.4 |
| TypeScript SDK generation | v0.3 |
| Visual workflow designer | v0.3 |

### 44.4 MVP Validation Criteria

The MVP is considered successful when:

1. **A developer** can create 4 Documents (Department, Employee, LeaveType, LeaveRequest) in ~200 lines of Go code.
2. **The admin UI** renders auto-generated forms and lists for all 4 Documents without any frontend code.
3. **The REST API** supports full CRUD for all 4 Documents with authentication and permission enforcement.
4. **The agent** can:
   - Answer "How many employees are in department X?" by querying the database.
   - Create a leave request when asked, with approval confirmation.
   - Respect permission boundaries (e.g., cannot read salary if the user's role doesn't permit it).
   - Operate on any Document without per-Document agent configuration.
5. **The audit log** records all operations (human and agent) with full traceability.
6. **The workflow** enforces leave request approval flow with role-based gating.

### 44.5 MVP Technical Architecture Summary

```
Single Go Binary
├── CLI (Cobra)
├── HTTP Server (Chi)
│   ├── REST API (/api/v1/document/*)
│   ├── RPC API (/api/v1/method/*)
│   ├── Agent API (/api/v1/agent/*)
│   ├── Meta API (/api/v1/meta/*)
│   └── Static Files (embedded React app)
├── Document Engine
│   ├── Schema Registry (compiled from Go structs)
│   ├── CRUD Operations
│   ├── Validation
│   ├── Hooks
│   └── Workflow Engine
├── Data Access Layer
│   ├── Query Builder
│   ├── PostgreSQL Dialect
│   └── SQLite Dialect
├── Permission Engine (RBAC)
├── Auth (JWT)
├── Agent Runtime
│   ├── Auto-generated Tools (from Registry)
│   ├── ReAct Planner
│   ├── Tool Executor
│   ├── Safety/Approval Layer
│   ├── OpenAI Provider
│   └── Anthropic Provider
├── Audit Log
└── Event Bus
```

**Estimated MVP implementation:** 15,000–25,000 lines of Go code + 5,000–8,000 lines of TypeScript/React.

---

*End of Document*

