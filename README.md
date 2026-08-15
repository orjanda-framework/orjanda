# Orjanda

**An agent-native business application framework written in Go.**

Define a business entity once as a Go struct with `oj` tags, and Orjanda derives
everything else from that single declaration — the database table, the CRUD API,
the admin UI, and the AI-agent tool definitions — all enforcing the *same*
permission rules.

> **Central thesis:** if a Document exists in the Orjanda Registry, it is
> automatically operable by both a human and an embedded AI agent, with no
> per-entity integration code.

```go
type LeaveRequest struct {
    schema.BaseDocument
    Reason   string `oj:"required"`
    Status   string `oj:"options=Draft|Submitted|Approved|Rejected,default=Draft"`
    Approved bool   `oj:"default=false"`
}
```

That struct alone gives you a `leave_requests` table, REST CRUD endpoints, a
metadata-driven form in the admin UI, and agent tools — simultaneously, from one
declaration.

---

## Why Orjanda

Business applications share ~80% of their infrastructure — CRUD, forms, lists,
permissions, search, audit — and every Go project rebuilds it by hand (PRD §3.1).
Adding an AI agent to such an app is a *second*, parallel engineering effort: a
tool definition, an input schema, a permission mapping, and error handling per
entity, which then drifts out of sync with the source schema (PRD §3.2).

Orjanda collapses this to O(1) per entity: **define the Document once, get the
database, API, UI, and agent layer for free** (PRD §2, §8). It is the closest
Go equivalent to Frappe or Django Admin, purpose-built for Go's type system and
for the reality that AI agents are first-class consumers of business
applications.

## Key Features

- **Code-first schemas** — Documents are Go structs annotated with `oj` tags,
  compiled into a read-only Registry at startup. No runtime-editable metadata
  (PRD §8.4).
- **One declaration → four layers** — a single Document yields the database
  table, REST + RPC API, metadata-driven admin UI, and agent tools, from the
  same compiled schema (PRD §9, §24).
- **Embedded AI Agent Runtime** — a ReAct agent that calls the *same*
  `document.*` functions as the REST API. No separate agent backend, no separate
  permission path (PRD §23.1, §25.1).
- **Automatic tool generation** — CRUD tools are generated per Document from the
  Registry at startup, and permission-checked per identity (TAD §10).
- **Secure by default** — JWT auth, RBAC permissions, human approval gates for
  agent write operations, and transactional audit logging (PRD §25, §28, §29).
- **PostgreSQL + SQLite** — pluggable dialect adapters with a custom query
  builder and Atlas/Goose-backed migrations (`orjanda migrate diff`/`up`).
- **Declared workflows & hooks** — lifecycle hooks (`before_save`,
  `after_insert`, …) and role-gated state machines (PRD §19).
- **Single binary** — the React admin UI is embedded in the Go binary via
  `embed.FS`; modular monolith, one deployable (PRD §9.1, §17.4).

## Architecture at a Glance

Orjanda is a **modular monolith**: one Go binary that embeds the REST/RPC API,
the admin UI, and the agent runtime. The compiled Registry is the single source
of truth every layer reads from.

```
              ┌──────────────────────────────────────────────┐
              │            Single Go binary                  │
              │  ┌──────────┐  ┌──────────┐  ┌─────────────┐ │
              │  │ Admin UI │  │ REST/RPC │  │ Agent Chat  │ │
              │  │ (React,  │  │ API (Chi)│  │ (WebSocket) │ │
              │  │ embedded)│  └────┬─────┘  └──────┬──────┘ │
              │  └──────────┘       │               │        │
              │            ┌───────▼───────────────▼───┐    │
              │            │   Agent Runtime (ReAct)    │    │
              │            └───────┬───────────────▲───┘    │
              │            ┌───────▼───────────────┴───┐    │
              │            │  Document Engine (CRUD,    │    │
              │            │  validation, hooks, perms) │    │
              │            └───────┬───────────────────┘    │
              │        ┌───────────▼───────────┐            │
              │        │  Registry · Perm ·    │            │
              │        │  Audit · Workflows    │            │
              │        └───────────┬───────────┘            │
              │                    ▼                        │
              │        ┌─────────────────────┐              │
              │        │ DAL (dialects:      │              │
              │        │  PostgreSQL, SQLite)│              │
              │        └─────────────────────┘              │
              └──────────────────────────────────────────────┘
```

Every read and write — REST, RPC, agent tool, workflow transition — passes
through the **same** `perm.Engine` (PRD §25.1). Full detail in the
[Technical Architecture Document](docs/ORJANDA-TAD.md).

## Getting Started

Orjanda is a **framework you consume, not a repository you clone**. You install
the `orjanda` CLI once, then `orjanda init` scaffolds *your own* Application — a
new Go module that imports Orjanda as a dependency. You build your application
on top of the framework; you never modify the framework source itself. This is
the same model as `django-admin startproject` or `bench init` in Frappe.

```
1. go install the orjanda CLI           →  you get the orjanda command
2. orjanda init myapp                   →  your new Application (a Go module)
3. cd myapp && ORJANDA_ENV=development orjanda serve   →  your app's dev server, on :8080
```

### Prerequisites

- **Go 1.26+**
- **Node.js 22+** (only if you are a framework contributor developing the
  admin UI — application users never need it, the UI ships embedded)

### Install the CLI

Install the `orjanda` command once with Go:

```bash
go install github.com/orjanda-framework/orjanda/cmd/orjanda@latest
```

That is the only installation step. You do **not** clone the Orjanda repository
to build an application. (Framework contributors who want to build the CLI from
source should see [Contributing](#contributing).)

### Scaffold your first Application

```bash
orjanda init myapp
cd myapp
```

`myapp` is *your* project — a new Go module built on top of Orjanda. This
generates `go.mod`, `main.go`, `app.go`, an `orjanda.yaml` (SQLite dev
defaults, with a commented `auth.jwt_secret` placeholder), and a `migrations/`
directory, with Orjanda wired in as a dependency.

### Add a Document

```bash
orjanda new document LeaveRequest --module=leave --submittable
```

Then open `documents/leave_request.go` and declare your fields:

```go
package documents

import (
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// LeaveRequest is a LeaveRequest business entity.
type LeaveRequest struct {
	schema.BaseDocument
	Reason   string `oj:"required"`
	Status   string `oj:"options=Draft|Submitted|Approved|Rejected,default=Draft"`
	Approved bool   `oj:"default=false"`
}

func (d *LeaveRequest) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "LeaveRequest",
		Module:      "leave",
		Submittable: true,
		Description: "Employee leave request",
	}
}

func (d *LeaveRequest) Get(field string) any {
	switch field {
	case "Reason":
		return d.Reason
	case "Status":
		return d.Status
	case "Approved":
		return d.Approved
	}
	return d.BaseDocument.Get(field)
}

func (d *LeaveRequest) Set(field string, value any) orjerrors.Error {
	switch field {
	case "Reason":
		if v, ok := value.(string); ok {
			d.Reason = v
			return nil
		}
	case "Status":
		if v, ok := value.(string); ok {
			d.Status = v
			return nil
		}
	case "Approved":
		if v, ok := value.(bool); ok {
			d.Approved = v
			return nil
		}
	}
	return d.BaseDocument.Set(field, value)
}
```

### Run it

Still inside your `myapp` directory, start your application's server. The
environment is chosen by `ORJANDA_ENV` (`development` is the default; set it
explicitly as documented):

```bash
ORJANDA_ENV=development orjanda serve
```

- The development server compiles the Registry, auto-creates missing tables,
  and starts on `http://127.0.0.1:8080` (SQLite by default).
- On first run it bootstraps a system administrator (`admin@localhost`) and
  prints a **one-time password** to stdout.
- When `auth.jwt_secret` is not configured, development `orjanda serve`
  generates an **ephemeral dev secret** (warns on startup) — fine for local
  exploration, but set a real secret in `orjanda.yaml` to keep sessions across
  restarts.
- For production, run `ORJANDA_ENV=production orjanda serve`: it fails fast on
  any Registry, migration, or stale-codegen error, never auto-creates tables,
  and requires a real `auth.jwt_secret` (see the [Configuration](#configuration)
  section and [TAD §16](docs/ORJANDA-TAD.md)).
- Open the admin UI at `http://127.0.0.1:8080` — `LeaveRequest` already appears
  in the sidebar with an auto-generated form and list.
- Open `http://127.0.0.1:8080/agent` to chat with the embedded agent, which can
  already query and create `LeaveRequest` records under the same permissions as
  the UI.

### Call the REST API

Log in to get a JWT, then use the standard CRUD endpoints:

```bash
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@localhost","password":"<one-time-password>"}' \
  | jq -r '.data.access_token')

curl http://127.0.0.1:8080/api/v1/document/LeaveRequest \
  -H "Authorization: Bearer $TOKEN"
```

REST routes live under `/api/v1/document/{doctype}`; see
[PRD §14](docs/ORJANDA-PRD.md) for the full API surface.

## CLI Reference

| Command | Description |
|---|---|
| `orjanda init <name>` | Scaffold a new Application (`go.mod`, `main.go`, `app.go`, `orjanda.yaml`, `migrations/`) |
| `orjanda new document <name>` | Generate a Document scaffold and register it in `app.go` |
| `orjanda new module <name>` | Generate a Module scaffold (`documents/hooks/workflows/api/ui`) |
| `orjanda serve` | Start the site. `ORJANDA_ENV` (or the `env` config key) selects the environment: `development` (default) auto-creates tables, warns-and-continues on Registry errors, and generates an ephemeral JWT secret if unset; `production` (`ORJANDA_ENV=production`) fails fast on any Registry, migration, or stale-codegen error and requires a real `auth.jwt_secret` |
| `orjanda migrate diff` | Generate migration SQL from schema changes |
| `orjanda migrate up` | Apply pending migrations |
| `orjanda migrate status` | Show migration status |
| `orjanda console` | Interactive REPL with the site context |
| `orjanda install <app>` / `uninstall <app>` | Run Application lifecycle hooks |
| `orjanda test` | Run application tests against an ephemeral SQLite database |
| `orjanda agent chat` | Terminal-based agent chat (great for testing) |
| `orjanda registry list` | List all registered Documents |
| `orjanda registry describe <doc>` | Show a Document's full compiled schema |

## Configuration

Configuration lives in `orjanda.yaml` (Viper), overridable by `ORJANDA_`
prefixed environment variables: the `env` deployment environment (`development`
or `production`, via `ORJANDA_ENV`), server port/host/CORS, the database driver
(`postgres` or `sqlite`) and DSN, the `auth.jwt_secret` JWT signing key (min 32
characters, via `ORJANDA_AUTH_JWT_SECRET`; development `orjanda serve` generates
an ephemeral key when it is absent — production requires a real one), and LLM
providers (OpenAI, Anthropic, and any OpenAI-compatible endpoint) plus agent
safety limits. See [TAD §1.3](docs/ORJANDA-TAD.md) for the authoritative schema.

## Documentation

| Document | Answers |
|---|---|
| [Product Requirements (PRD)](docs/ORJANDA-PRD.md) | *Why* and *what* — goals, decisions, rationale, worked examples |
| [Technical Architecture (TAD)](docs/ORJANDA-TAD.md) | *Exact shape* — interfaces, data flows, algorithms, contracts |

## Project Status

Orjanda is a **pre-1.0 MVP**, developed against the scope in
[PRD §44](docs/ORJANDA-PRD.md). The MVP feature set — schema system, Registry,
document engine, PostgreSQL + SQLite, migrations, REST/RPC API, JWT auth, RBAC
permissions, admin UI, embedded agent runtime, auto tool generation, approval
gates, audit log, hooks, workflows, and the `orjanda` CLI — is implemented and
covered by unit and integration tests.

Post-MVP scope (multi-tenancy, MySQL, OpenTelemetry, background jobs, MCP
server, and more) is explicitly deferred per [PRD §44.3](docs/ORJANDA-PRD.md).

## Versioning

Orjanda follows **Semantic Versioning** (`MAJOR.MINOR.PATCH`). Until 1.0,
releases use `0.x.y` where minor bumps may include breaking changes. The
[CHANGELOG](CHANGELOG.md) tracks releases; the first public release will be
`v0.1.0`.

## License

Orjanda is licensed under the **Apache License 2.0**. See [LICENSE](LICENSE).

## Contributing

Orjanda is developed as a framework in its own repository. **Application
developers never clone it** — if you want to work on the framework itself,
welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first — it covers
development setup, build/test commands, formatting, and the branch/PR workflow.

To build the `orjanda` CLI from framework source:

```bash
git clone https://github.com/orjanda-framework/orjanda.git
cd orjanda
go build -o orjanda ./cmd/orjanda
```

Report bugs and request features via the [issue templates](.github/ISSUE_TEMPLATE/). This
project adheres to the [Contributor Covenant](CODE_OF_CONDUCT.md). For security
vulnerabilities, follow the responsible-disclosure process in
[SECURITY.md](SECURITY.md).

---

*Orjanda — if a Document exists in the Registry, it is automatically operable by
both a human and an embedded AI agent.*
