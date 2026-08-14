# Configuration Reference

Every setting the Orjanda binary reads. The authoritative schema is TAD §1.3;
this document is the user-facing reference. Configuration is loaded by
`config.Load` from `orjanda.yaml` (or `--config <path>`) and/or
`ORJANDA_`-prefixed environment variables, which **override** file values.

## Load order

1. Built-in defaults (below) are applied first.
2. Values from the config file (`orjanda.yaml`, or `--config`) override them.
3. `ORJANDA_*` environment variables override file values.

The reference `orjanda.yaml` ships in the repo root and every key is
env-interpolated — no real secrets live in the file.

## Environment variables

Env names derive from Viper keys by uppercasing and replacing dots with
underscores, prefixed with `ORJANDA_`. For example:

| Key | Env var |
|---|---|
| `server.port` | `ORJANDA_SERVER_PORT` |
| `auth.jwt_secret` | `ORJANDA_AUTH_JWT_SECRET` |
| `database.dsn` | `ORJANDA_DATABASE_DSN` |
| `llm.providers.openai.api_key` | `ORJANDA_LLM_PROVIDERS_OPENAI_API_KEY` |

Two convenience aliases also exist: `ORJANDA_OPENAI_API_KEY` →
`llm.providers.openai.api_key` and `ORJANDA_ANTHROPIC_API_KEY` →
`llm.providers.anthropic.api_key` (matching the `${ORJANDA_OPENAI_API_KEY}`
interpolation used in `orjanda.yaml`).

## Reference table

### `server`

| Key | Type | Default | Notes |
|---|---|---|---|
| `server.port` | int | `8080` | TCP port for the HTTP server (REST/RPC/UI). Must be 1–65535. |
| `server.host` | string | `0.0.0.0` | Bind address. The scaffolded app uses `127.0.0.1`. |
| `server.cors_origins` | []string | `["*"]` | Allowed CORS origin patterns. |

### `database`

| Key | Type | Default | Notes |
|---|---|---|---|
| `database.driver` | string | `sqlite` | Must be `sqlite` or `postgres`. MySQL is post-MVP (PRD §44.3). |
| `database.dsn` | string | `orjanda.db` | Driver connection string. SQLite: a file path. Postgres: `postgres://user:pass@host:5432/db?sslmode=disable`. |
| `database.max_open_conns` | int | `25` | Connection pool size. |
| `database.max_idle_conns` | int | `5` | Idle connections in the pool. |

### `auth`

| Key | Type | Default | Notes |
|---|---|---|---|
| `auth.jwt_secret` | string | **none (required)** | HMAC-SHA256 signing key for access/refresh JWTs. **Must be ≥ 32 characters.** There is deliberately no default — a derived or hardcoded key would let anyone forge administrator tokens. Set via `auth.jwt_secret` or `ORJANDA_AUTH_JWT_SECRET`; validation lives in `config.ValidateJWTSecret`. |

### `llm`

| Key | Type | Default | Notes |
|---|---|---|---|
| `llm.default_provider` | string | `openai` | Key into `llm.providers` used when no provider is specified. |
| `llm.providers.<name>.api_key` | string | *(env)* | Provider secret. Use `ORJANDA_OPENAI_API_KEY` / `ORJANDA_ANTHROPIC_API_KEY` (or the `ORJANDA_LLM_PROVIDERS_*` form). Never commit real keys. |
| `llm.providers.<name>.model` | string | `openai`: `gpt-4o`; `anthropic`: `claude-3-5-sonnet-20240620` | Model identifier. |
| `llm.providers.<name>.max_tokens` | int | `4096` | Cap per LLM completion request. |
| `llm.providers.<name>.base_url` | string | *(empty → provider default)* | Endpoint override. **Required** for the `openai_compatible` provider. |
| `llm.providers.<name>.auth` | string | *(empty → provider default)* | Auth mode: `bearer` (always send `Authorization: Bearer`), `bearer_if_key` (only when `api_key` is set), or `none`. Honored by the OpenAI and `openai_compatible` adapters. |
| `llm.providers.<name>.tool_calling` | bool | *(nil → adapter default)* | Override the adapter's tool-calling capability report (self-hosted OpenAI-compatible servers may lack it). |
| `llm.providers.<name>.structured_output` | bool | *(nil → adapter default)* | Override the adapter's structured-output capability report. |
| `llm.safety.max_bulk_operations` | int | `5` | Above this record count, a bulk agent operation **always** requires human approval (TAD §12.1 step 2). This approval gate cannot be disabled — only the threshold is configurable (PRD §28.1). |

## Example

The repo-root `orjanda.yaml` is the reference example. A minimal SQLite dev
setup:

```yaml
server:
  host: 127.0.0.1
  port: 8080

auth:
  jwt_secret: ${ORJANDA_AUTH_JWT_SECRET}   # required, ≥ 32 chars

database:
  driver: sqlite
  dsn: orjanda.db

llm:
  default_provider: "openai"
  providers:
    openai:
      api_key: ${ORJANDA_OPENAI_API_KEY}
      model: "gpt-4o"
      max_tokens: 4096
```

## Validation

`config.Load` fails fast (before the server starts) when:

- `database.driver` is not `postgres` or `sqlite`,
- `server.port` is outside [1, 65535],
- `auth.jwt_secret` is missing or shorter than 32 characters.

## Per-environment values

Set `ORJANDA_*` vars differently per environment (development, CI, production)
without editing config files — for example
`ORJANDA_DATABASE_DSN`, `ORJANDA_AUTH_JWT_SECRET`,
`ORJANDA_OPENAI_API_KEY` in CI/containers, and local `orjanda.yaml` values for
day-to-day development.
