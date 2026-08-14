# Security Policy

Orjanda takes security seriously. This document describes how security
vulnerabilities should be reported to the project and how they are handled.

## Supported Versions

Orjanda is currently **pre-1.0** with no tagged releases yet. While the project
is in this stage, **all** versions of the framework are supported for security
reports — report a vulnerability regardless of which development version you are
running. Once tagged releases begin (starting with `v0.1.0`), this section will
list which releases receive security fixes.

## Reporting a Vulnerability

**Do not open a public GitHub issue for a security vulnerability.**

Please report suspected vulnerabilities privately. The **sole official
reporting channel** is a **private security advisory** via GitHub's Security tab
at `https://github.com/orjanda-framework/orjanda/security/advisories/new`. This
works without revealing a personal email address and keeps the issue hidden
until it is resolved.

### What to include in your report

To help triage quickly, please include:

- The Orjanda version or commit SHA you are running (e.g. from `go.mod`).
- The affected package or file path (e.g. `perm/`, `auth/`, `agent/safety/`).
- The database dialect in use (`postgres` or `sqlite`), if relevant.
- A minimal, reproducible description of the vulnerability.
- The security impact you believe it has (e.g. privilege escalation,
  information disclosure, denial of service).
- Whether the vulnerability is already publicly known.

### What happens next

- The maintainer will acknowledge your report within **48 hours**.
- You will receive a status update as the issue is triaged and addressed.
- Fixes are coordinated in private until a release that includes them is ready,
  to give users time to update before details are disclosed.

## Scope

This policy covers the Orjanda framework core, including:

- The permission engine and permission enforcement (`perm.Engine`, PRD §25.1 —
  the single permission path for REST, RPC, agent tools, and workflow
  transitions).
- Authentication and authorization (JWT issuance and validation, `auth.jwt_secret`
  handling, role assignment).
- The agent safety layer (approval gates, bulk-operation limits,
  sensitive-field masking; PRD §25.3).
- The document engine, validation, and audit logging.
- Configuration handling, especially `ORJANDA_`-prefixed environment variables.

Out of scope are vulnerabilities in third-party dependencies themselves — please
report those upstream — and issues in applications built *on top of* Orjanda
(those belong to the application's own security process).

## Security-Relevant Configuration

The following defaults are security-relevant and must not be weakened:

- `auth.jwt_secret` is **required** and must be at least **32 characters**
  (there is deliberately no default — see `config.ValidateJWTSecret`). It should
  be supplied via `ORJANDA_AUTH_JWT_SECRET`, never committed to source control.
- The agent approval gate for bulk operations is **not** configurable to
  "always" (PRD §28.1, TAD §12.1).

## Acknowledgements

We are grateful to everyone who helps keep Orjanda secure. A public list of
security researchers credited for disclosures may be maintained here once the
project has its first reports.
