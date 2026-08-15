# Getting Started with Orjanda

Build a working Orjanda application — a `Document` definition, its database
table, REST API, admin UI, and an AI agent that can operate on it — in about 15
minutes. This guide mirrors the developer workflows in PRD §37 (creating an
application) and §38 (agent workflow).

## Prerequisites

- **Go 1.26+** (`go version`).
- No CGo required — the SQLite driver is pure Go.
- A working `orjanda` binary. Two ways to get one:

  ```bash
  # Published CLI (once the first release tag exists)
  go install github.com/orjanda-framework/orjanda/cmd/orjanda@latest

  # From a local framework checkout (today's path — no tag exists yet)
  cd /path/to/orjanda && go build -o /usr/local/bin/orjanda ./cmd/orjanda
  ```

  Check it works: `orjanda --help`.

## 1. Scaffold an application

```bash
orjanda init myapp
cd myapp
```

This creates (TAD §16 / PRD §21.2):

```
myapp/
├── go.mod           # module + require github.com/orjanda-framework/orjanda
├── main.go          # entry point → cli.Main(configure)
├── app.go           # registers Documents (kept in sync by `new document`)
├── orjanda.yaml     # dev defaults: env: development, sqlite at :8080 (+ commented auth.jwt_secret)
├── migrations/      # Goose migration files (written by `migrate diff`)
└── .orjanda.json    # CLI manifest recording generated Documents
```

> **Local-checkout note:** if you built the CLI from a framework checkout, the
> scaffolded `go.mod` gets a `replace` directive pointing back at it (or set
> `ORJANDA_FRAMEWORK_PATH`). Once the framework module is published this is
> automatic via the module proxy.

## 2. Add a Document

`new document` writes `documents/{snake_case}.go` and registers it in `app.go`:

```bash
orjanda new document LeaveRequest
```

Now open `documents/leave_request.go` and declare the business fields
(schema declared once — the DB table, REST API, admin UI, and agent tools all
derive from it, PRD §10.1):

```go
package documents

import (
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

type LeaveRequest struct {
	schema.BaseDocument
	Reason   string `oj:"required"`
	Status   string `oj:"options=Draft|Submitted|Approved|Rejected,default=Draft"`
	Approved bool   `oj:"default=false"`
}

func (d *LeaveRequest) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "LeaveRequest",
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

`Link` fields reference another Document; `ChildTable` fields (`[]MyChild`)
declare child records; see the full tag reference in TAD §2.1.

## 3. Create and apply migrations

`serve` auto-creates tables for local development, but for a real project use
the Migrator (TAD §14). Diff the compiled Registry against the database, then
apply:

```bash
orjanda migrate diff          # writes migrations/{timestamp}_*.sql
orjanda migrate status        # shows what is applied
orjanda migrate up            # applies pending migrations
```

Destructive changes (dropped columns/tables) are gated behind
`orjanda migrate diff --allow-destructive` — they are never included silently.

## 4. Run the server

`orjanda serve` starts the site in the environment selected by `ORJANDA_ENV`
(or the `env` config key, default `development`):

```bash
ORJANDA_ENV=development orjanda serve
```

On first run it prints a one-time bootstrap credential:

```
bootstrapped system administrator
admin password: <16 random characters>
```

The development server auto-creates missing tables and is forgiving of Registry
errors (PRD §21 / TAD §16). If `auth.jwt_secret` is not set, it generates an
ephemeral dev secret and warns on startup (sessions won't survive a restart) —
uncomment `auth.jwt_secret` in `orjanda.yaml` with a ≥ 32-character value to
keep them. For production behavior use `ORJANDA_ENV=production orjanda serve`:
it never auto-creates tables, requires pre-applied migrations, and fails fast on
any Registry, migration, or stale-codegen error (TAD §16).
Open the **admin UI**:

- **http://localhost:8080/** — the embedded React admin UI (SPA). Log in as
  `admin@localhost` with the printed password. Every Document gets a
  metadata-driven list page and form automatically (PRD §17).

## 5. Exercise the REST API

The same data is available over REST (PRD §14.5 envelope):

```bash
# 1. Log in — get an access token
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@localhost","password":"<printed password>"}' \
  | jq -r .data.access_token)

# 2. Create a LeaveRequest
curl -s -X POST http://localhost:8080/api/v1/document/leave_request \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"reason":"Vacation","status":"Submitted"}'

# 3. List documents of a type
curl -s http://localhost:8080/api/v1/document/leave_request \
  -H "Authorization: Bearer $TOKEN"

# 4. Read, update, delete a single record
curl -s http://localhost:8080/api/v1/document/leave_request/{id} -H "Authorization: Bearer $TOKEN"
curl -s -X PATCH http://localhost:8080/api/v1/document/leave_request/{id} \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"status":"Approved"}'
curl -s -X DELETE http://localhost:8080/api/v1/document/leave_request/{id} -H "Authorization: Bearer $TOKEN"
```

Endpoints: `GET/POST /api/v1/document/{doctype}` for list/create,
`GET/PATCH/PUT/DELETE /api/v1/document/{doctype}/{id}` for read/update/delete,
`POST /api/v1/auth/login` and `/auth/refresh` for auth, `/api/v1/meta` for
metadata, and `/api/v1/method/{app}.{module}.{method}` for custom RPC methods.

## 6. Talk to the embedded agent

With an LLM provider configured (see below), the agent runtime drives the same
Document Engine as the API — no per-Document agent integration (PRD §23):

```bash
orjanda agent chat --user admin
```

Then in the terminal prompt: *"How many leave requests are there?"* or *"Create
a leave request for me and submit it."* Write operations go through approval
gates. The admin UI also has an agent chat panel (PRD §38).

### Configure an LLM provider

Add your provider to `orjanda.yaml` (keys match TAD §1.3; secrets come from
`ORJANDA_*` env vars, never from the file):

```yaml
llm:
  default_provider: "openai"
  providers:
    openai:
      api_key: ${ORJANDA_OPENAI_API_KEY}   # set in the environment
      model: "gpt-4o"
      max_tokens: 4096
```

Supported providers: `openai`, `anthropic`, `openai_compatible` (TAD §1.3).

## Next steps

- **Configuration reference** — every key in `orjanda.yaml` and its
  `ORJANDA_*` override: see `docs/configuration.md`.
- **Architecture tour** — Document → Registry → Engine → API/UI/agent pipeline:
  see `docs/architecture.md`.
- **The full product spec** — `docs/ORJANDA-PRD.md` (§37 developer workflow,
  §38 agent workflow) and `docs/ORJANDA-TAD.md` (exact interfaces).
- **Contributing** — see `CONTRIBUTING.md` if you want to improve the framework
  itself, and `docs/development.md` for the deep technical guide.
