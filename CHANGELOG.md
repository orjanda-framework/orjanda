# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> **Versioning note:** Orjanda is pre-1.0 and follows the SemVer 0.x convention —
> **minor (0.x) releases may contain breaking changes**; patch releases are
> backward-compatible. The first release is proposed as `0.1.0` (pending
> approval) — no tag exists yet, so all current work lives under `[Unreleased]`.

## [Unreleased]

### Added

- **Schema system** — declare a business entity once as a Go `Document` struct
  with `oj` tags; child tables and Links (PRD §44.2).
- **Registry** — compiles Go structs into read-only in-memory metadata at
  startup.
- **Document Engine** — Create, Read, Update, Delete, List, and Search with
  validation.
- **Database layer** — PostgreSQL and SQLite dialects (pure-Go SQLite via
  `modernc.org/sqlite`), custom query builder, dialect adapters, and the
  transactional Migrator (`orjanda migrate diff` / `orjanda migrate up`).
- **REST API** — full CRUD + List + Search for all Documents under
  `/api/v1/document/*`.
- **RPC API** — custom method registration and invocation under
  `/api/v1/method/*`.
- **Authentication** — JWT-based, email/password with bcrypt hashing, role
  assignment, bootstrap admin provisioning.
- **Permissions** — document-level RBAC (read/write/create/delete per role),
  enforced through a single `perm.Engine` (PRD §25.1).
- **Admin UI** — React + Tailwind, auto-generated form and list pages for all
  Documents, plus an agent chat interface (embedded via `embed.FS`).
- **Agent Runtime** — embedded ReAct loop with tool calling and per-turn
  provider injection.
- **Auto tool generation** — CRUD tools generated per Document from the
  Registry.
- **LLM providers** — OpenAI, Anthropic, and OpenAI-compatible providers.
- **Safety** — approval gates for write operations and bulk-operation limits.
- **Audit log** — DB-backed transactional audit logging with agent flag.
- **Hooks** — document lifecycle hooks (`before_save`, `after_insert`, etc.).
- **Workflows** — basic state machine with role-gated transitions.
- **CLI** — `init`, `serve`, `new document`, `new module`, `migrate diff/up`,
  `agent chat`, `registry list/describe`, `install`, `uninstall`, `test`.
- **Testing harness** — first-class `orjanda/testing` package
  (`NewTestSite`, `WithApps`, `MockLLM`, testcontainers-backed PostgreSQL
  integration lane).
- **Full-text search** — dialect FTS backend (SQLite FTS5, PostgreSQL
  `tsvector`) used by the Document `q` filter.
- **Lifecycle hooks in `app.Definition`** — application install/uninstall
  support.
- **WebSocket agent chat** — origin authorization and session management with
  TTL.
- **Metadata API** — Document type listing and metadata retrieval (with
  gated-field protection).
- **Code generation** — `orjanda-codegen.mjs` TypeScript SDK generation for
  the admin UI.

### Changed

- Child table naming standardized to pluralized `snake_case`, derived at
  compile time (breaking for any pre-release schema — none exist yet).
- Agent tool execution now enforces role checks through `perm.Engine` for both
  standard and custom tools.
- **Deployment model unified behind `ORJANDA_ENV`:** the `bench` command is
  removed. `orjanda serve` now serves both environments, selected by
  `ORJANDA_ENV=development|production` (or the new `env` config key, default
  `development`). `development` preserves the old `serve` behavior —
  auto-create tables, warn-and-continue on Registry errors, ephemeral JWT
  secret when unset — while `production` preserves every `bench` safety
  guarantee: fail-fast on any Registry/migration/codegen error, no auto-create,
  no admin bootstrap, and a mandatory real `auth.jwt_secret`. `config.Load` is
  now the single loader for both environments (the dev-only `config.LoadDev` is
  gone).
- `orjanda serve` boots a scaffolded app without a configured
  `auth.jwt_secret` in the `development` environment by generating an ephemeral
  dev secret (warned on startup; sessions don't survive restart). Production
  requires a real secret — dev-only convenience, production behavior unchanged.

### Fixed

- Order-by column validation and direction hardening to prevent injection
  attacks.
- WebSocket origin authorization for connections.
- `List` `q` filter now resolves through full-text search.
- Concurrency-safe bootstrap to prevent duplicate admin creation.

### Security

- JWT authentication with configurable, validated secret
  (`config.ValidateJWTSecret`, minimum 32 chars).
- Secure-by-default: new capabilities ship permission-checked and
  audit-logged.
- No secrets committed — configuration is env-interpolated
  (`ORJANDA_*` variables).

## [0.1.0] — Proposed first release (pending approval)

> No tag exists yet. This section is a placeholder for the first public
> release; when the tag is cut, all entries above move here and release links
> are added below.

[Unreleased]: https://github.com/orjanda-framework/orjanda (release compare links are added once tags exist)
