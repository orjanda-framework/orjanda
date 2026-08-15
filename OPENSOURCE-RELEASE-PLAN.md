# Orjanda — Public Open-Source Release Preparation Plan

## Repository findings (basis for the plan)

- **State:** Substantially implemented Go framework (module `github.com/orjanda-framework/orjanda`, `go 1.26.5`) — `errors`, `config`, `schema`, `dal` (postgres+sqlite+migrator), `cache`, `document`, `event`, `perm`, `workflow`, `audit`, `auth`, `orjanda-core`, `api` (REST/RPC/middleware/WS), `agent` (llm/tools/runtime/planner/safety), `ui`, `server`, `cli`, `cmd/orjanda`, `testing`, plus `orjanda-ui` (React 19 + Vite + Vitest, committed `dist`). AGENTS.md still says "documentation-only, pre-implementation" — that is **stale**.
- **README.md:** one line (`# orjanda`) — placeholder.
- **No** LICENSE, CHANGELOG, CONTRIBUTING, SECURITY, SUPPORT, GOVERNANCE, CODE_OF_CONDUCT, CODEOWNERS, issue/PR templates. No license headers in any `.go` file.
- **CI already exists:** `.github/workflows/ci.yml` covers `go build ./...`, `go vet`, `go test -race`, golangci-lint (gofmt included), integration (testcontainers, `integration` tag), and the frontend lane — already aligned with local tooling.
- **Version conventions:** PRD v1.0.0, TAD v1.1.0, `orjanda-ui` 0.1.0. **No git tags**, no CHANGELOG convention, no release process.
- **Docs:** `docs/ORJANDA-PRD.md`, `docs/ORJANDA-TAD.md`. The `docs/ORJANDA-IMPLEMENTATION-PLAN.md` referenced by AGENTS.md **was deleted** in the latest commit `bcb9fd1`, as was `docs/REVIEW-2026-08-12.md` (an internal code-review report — see hygiene task). Both still exist in git history.
- **Secrets check:** No `.env`, no credentials in the working tree. `orjanda.yaml` is a safe example (env interpolation, placeholder DSN). `.gitignore` already covers `.env`, `orjanda.local.yaml`, node_modules, etc.
- **Identity:** git author is `mohmaed Kamal <mohamed.mohamed1996318@gmail.com>`; remote is `github.com/orjanda-framework/orjanda`. No public contact info is committed anywhere.
- **Inconsistency:** `.gitignore` ignores `.vscode/` but `.vscode/settings.json` is tracked.

---

## P0 — Essential before making the repository public

### Task 1 — `README.md` — **UPDATE** (placeholder → full)

- **Purpose:** First impression for a first-time GitHub visitor; the primary marketing/onboarding artifact.
- **Must contain:** What Orjanda is (agent-native business application framework in Go; one `Document` struct with `oj` tags → DB, REST/RPC API, admin UI, and AI-agent tools automatically, one permission model — preserve this framing verbatim from PRD §7/§8); the problem it solves (PRD §3); key features (PRD §4/§10); high-level architecture (PRD §9 — modular monolith, single binary, embedded UI + Agent Runtime); installation & quick start (Go 1.26+, `go install`/`go run`, `orjanda init`/`new document` scaffold — use the CLI scaffold, since the HR example app was removed); first application example (a small `LeaveRequest`-style Document using `oj` tags); CLI usage table (`serve` — with `ORJANDA_ENV` development/production — `migrate diff/up/status`, `console`, `init`, `new document/module`, `install`, `uninstall`, `test`, `agent chat`, `registry list/describe` — from `cli/`); documentation links (`docs/ORJANDA-PRD.md`, `docs/ORJANDA-TAD.md`, the new `docs/*` guides); project status (pre-v1.0 MVP, per PRD §44); version; license (matches Task 2); contribution entry points (CONTRIBUTING.md, issue templates).
- **Reference:** `docs/ORJANDA-PRD.md` §1–§10, §21, §36–§38; `docs/ORJANDA-TAD.md` §1.4, §16; `cli/root.go`, `cli/doc.go`; `config/config.go`.
- **Dependencies:** Task 2 (license name must match LICENSE), Task 4/5 (linked files), Task 16–19 (docs links) — write links defensively, finalize wording after those exist.
- **Validation:** A new visitor can go from "what is it" to a running `serve` in under 5 minutes with no prior context; every command/link/flag is accurate; no invented claims (no unreleased features, no false status).

### Task 2 — `LICENSE` — **NEW** (approval required)

- **Purpose:** Legal basis for distribution and contributions.
- **Recommendation — Apache License 2.0.** Rationale: (1) it is the dominant license for Go frameworks/libraries, so it maximizes adoption and is understood by enterprise users; (2) explicit patent grant protects contributors, which matters for a framework others embed; (3) permissive, so downstream business apps built on Orjanda face no copyleft obligations (matching the "framework people build products on" goal in PRD §3.4); (4) compatible with the project's "open-source framework" classification in PRD §1. Alternatives: MIT (simpler, weaker patent clause) or MPL-2.0 (file-level copyleft, popular in the Go ecosystem but more restrictive for embedded use). **Decision required.**
- **Must contain:** Full Apache-2.0 text (or chosen license), correct copyright line. Suggested holder: "Orjanda Framework Authors" or the maintainer's legal name — **decision required** (do not invent an entity).
- **Dependencies:** None.
- **Validation:** `LICENSE` parses; GitHub auto-detects the license on the repo page; README license badge/section matches; (optional, later) add SPDX headers to `.go` files — keep out of scope here.
- **Gate:** Blocks publication. All other tasks can proceed; only publication is gated.

### Task 3 — `SECURITY.md` — **NEW**

- **Purpose:** Responsible-disclosure route so vulnerabilities are not posted in public issues.
- **Must contain:** Supported-version statement (current pre-v1.0: report regardless of version); how to report — **private channel only**; do NOT file public issues; expected timeline for acknowledgement/fix (suggest acknowledge ≤48h); what to include (Orjanda version, affected package/file, minimal repro, impact); scope (framework core, ORJANDA-* config, agent safety layer, permission model); hall of thanks (optional, TBD). **Do not invent an email.** Add a placeholder like `<security contact: TBD — set in approval step>` for the security email/address (e.g., private GitHub security advisories, which work without an email).
- **Reference:** PRD §25 (permission/security model), §8.6 (secure by default).
- **Dependencies:** Task 2 (copyright/license framing not strictly needed).
- **Validation:** Instructions are unambiguous; no fabricated contact; GitHub "Security" tab shows the policy once published.

### Task 4 — `CONTRIBUTING.md` — **NEW**

- **Purpose:** Onboard contributors and set expectations.
- **Must contain:** Dev setup (Go 1.26+, Node 22 for `orjanda-ui`, no CGo needed for SQLite); build/test commands — mirror exactly what CI runs: `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...`, `go test -tags integration ./testing/...`, `golangci-lint run` (config in `.golangci.yml`), `npm ci && npm run typecheck && npm test && npm run build` in `orjanda-ui/`; formatting (gofmt via golangci config; `gofmt -l .`); branch/PR workflow (fork → feature branch → PR against `main`; commit message conventions observed in history — `feat:`, `fix:`, `docs:`, `refactor:`); expectations (tests for changes, no parallel permission paths — PRD §25.1, no new error types — TAD §1.1, cite PRD/TAD section numbers instead of re-explaining — AGENTS.md §7); architecture guidelines (new packages only inside the TAD §5.1 layout; extension points per TAD §9; code-first schemas only, PRD §8.4).
- **Reference:** `AGENTS.md` (workflow checklist, conventions §6, boundaries §7), `docs/ORJANDA-PRD.md` §8/§25, `docs/ORJANDA-TAD.md` §1/§5/§9, `.github/workflows/ci.yml`.
- **Dependencies:** Task 2 (license headers/CLA implications for contributions); Task 1 (project framing).
- **Validation:** A contributor can set up and run the full suite from this file alone; every command exists and passes locally.

### Task 5 — `CODE_OF_CONDUCT.md` — **NEW**

- **Purpose:** Standard community norms.
- **Must contain:** **Contributor Covenant v2.1** (established standard; do not invent a custom policy) with the core contact placeholder `<maintainer contact: TBD — must be supplied>`.
- **Reference:** None needed (use the canonical Contributor Covenant text).
- **Dependencies:** Task 4 (companion).
- **Validation:** Matches canonical Contributor Covenant; enforcement contact resolves to a real maintainer address after approval.

---

## P1 — Project maintenance and community

### Task 6 — `CHANGELOG.md` — **NEW**

- **Purpose:** Establish release-notes structure for v0.x.
- **Must contain:** **Keep a Changelog** format with `[Unreleased]` section; first entry listing the initial public release contents (framework core, REST/RPC/WS API, admin UI, agent runtime, CLI, testing harness) per PRD §44 MVP scope; note the project follows SemVer pre-1.0 (0.x minor = breaking) and per-commit `feat:`/`fix:`/`docs:` categories from git history; links to releases once tags exist (none yet — do not invent a version; use `[Unreleased]` only, with `0.1.0` proposed as the first tag **pending approval**).
- **Reference:** git log (`git log --oneline` gives the feature inventory), PRD §44 (MVP scope), `orjanda-ui/package.json` (0.1.0 precedent).
- **Dependencies:** Task 1 (project framing), Task 2 (version/license framing).
- **Validation:** Structure matches Keep a Changelog; entries trace to real commits; no invented versions/dates.

### Task 7 — `SUPPORT.md` — **NEW**

- **Purpose:** Route questions to the right places.
- **Must contain:** Bug reports → GitHub Issues (link to Task 10 template); feature requests → GitHub Issues (Task 11); security → SECURITY.md; usage questions — **do not invent channels** — propose GitHub Discussions *only if the maintainer enables it* (**decision required**); otherwise direct to Issues; no email/forum claims that don't exist. Community resources table marked with actual availability.
- **Reference:** Task 3, 10, 11.
- **Dependencies:** Tasks 3, 10, 11.
- **Validation:** Every channel listed actually exists and is enabled; no fabricated Discord/Slack/forum links.

### Task 8 — `GOVERNANCE.md` — **NEW**

- **Purpose:** Define decision-making for a project that starts with a single maintainer.
- **Must contain:** Current model: benevolent-dictator / single-maintainer model with a defined growth path; maintainer = `<name — TBD>` (placeholder); how contributions are accepted (maintainer reviews PRs, lazy-consensus for minor changes); how major architectural decisions are made (RFC-style: PRD/TAD are the source of truth per AGENTS.md §2 — changes that alter interfaces/packages must first amend PRD/TAD, then be sequenced per the implementation plan); how new maintainers can be added; how conflicts are resolved; release process (maintainer cuts version tags, follows CHANGELOG).
- **Reference:** `AGENTS.md` §2 (docs-as-source-of-truth rule), `docs/ORJANDA-TAD.md` §5.1 (package DAG).
- **Dependencies:** Tasks 1, 4.
- **Validation:** Roles/decision rules are explicit; no invented people or committees.

### Task 9 — `CODEOWNERS` — **NEW**

- **Purpose:** Assign review responsibility.
- **Must contain:** Initial rule mapping directories to owners, e.g. `/agent/`, `/api/`, `/dal/`, `/perm/`, `/schema/`, `/orjanda-ui/`, `/docs/`, root-level files. **Do not invent GitHub usernames.** Use a placeholder for the maintainer handle, e.g. `* @<maintainer-handle-TBD>` and note all paths default to the maintainer until the handle is supplied.
- **Reference:** Directory layout from `ls` (actual tree) and TAD §5.1.
- **Dependencies:** Task 2/4 (ownership context).
- **Validation:** Renders without errors once the handle is supplied; patterns match real directories.

### Task 10 — `.github/ISSUE_TEMPLATE/bug_report.yml` — **NEW**

- **Purpose:** Structured bug reports for a Go framework.
- **Must contain:** Form schema (YAML): title, description; required fields — Orjanda version (from `go.mod` / commit SHA — no tags exist yet), Go version, database dialect (postgres/sqlite), environment (local/CI/Docker), steps to reproduce, expected vs. actual behavior, relevant logs (sanitized — no secrets), affected subsystem (Document/permissions/agent/API/UI/CLI/migrations); optional: proposed fix. Checklist reminding users to search existing issues and to **not** include secrets.
- **Reference:** `go.mod`, existing issue-less history, PRD §32 (testing conventions).
- **Dependencies:** None.
- **Validation:** Form renders in GitHub UI; fields match real subsystems; no invented data.

### Task 11 — `.github/ISSUE_TEMPLATE/feature_request.yml` — **NEW**

- **Purpose:** Structured feature requests.
- **Must contain:** Form schema: problem statement, proposed behavior, why it fits Orjanda's scope (link to PRD §5 Non-Goals to filter out-of-scope asks like plugins/MCP/MySQL), affected area, alternatives considered, whether the requester can contribute a PR. Note that changes touching interfaces/architecture must go through PRD/TAD first (Task 4/8).
- **Reference:** PRD §5, §44.3 (MVP non-scope), `AGENTS.md` §7.
- **Dependencies:** None.
- **Validation:** Form renders; scope-guard text matches PRD §5.

### Task 12 — `.github/ISSUE_TEMPLATE/config.yml` — **NEW**

- **Purpose:** Tame issue volume.
- **Must contain:** `blank_issues_enabled: false` (or true — small choice), `contact_links` pointing to real resources only: SECURITY.md (vulnerabilities) and any enabled Discussions channel (**decision required**; otherwise omit).
- **Reference:** Task 3, 7.
- **Dependencies:** Tasks 3, 7.
- **Validation:** Links resolve to files that exist after tasks 3/7.

### Task 13 — `.github/PULL_REQUEST_TEMPLATE.md` — **NEW**

- **Purpose:** Consistent PRs.
- **Must contain:** Markdown template: summary, type of change (feat/fix/docs/refactor/chore), related issue(s), testing performed (with exact commands — mirrors Task 4), checklist (go vet/build/test pass; `golangci-lint` clean; `gofmt`; UI typecheck/tests pass if UI touched; tests added for changed behavior; **no** secrets; PRD/TAD section cited where behavior depends on a spec; changelog entry added per Task 6).
- **Reference:** `.github/workflows/ci.yml`, `.golangci.yml`, Task 4.
- **Dependencies:** Tasks 4, 6.
- **Validation:** Checklist matches what CI actually runs.

---

## P1 — Automation

### Task 14 — `.github/workflows/ci.yml` — **REVIEW / UPDATE**

- **Purpose:** Keep the public repo green; align with local commands.
- **Status:** Existing workflow already covers build, vet, `-race` unit tests, golangci-lint (incl. gofmt formatter), testcontainers integration, and the frontend lane. **Minimal changes only:**
  - Add a formatting gate `gofmt -l .` (make gofmt an explicit failure independent of lint) and `go mod tidy` drift check (`go mod tidy` then `git diff --exit-code go.mod go.sum`).
  - Optionally add `concurrency:` cancellation for PR pushes and `timeout-minutes`.
  - Keep the `integration` job gated behind the `integration` tag and Docker, as-is.
  - No new steps beyond what passes locally today.
- **Reference:** Existing `ci.yml`, `.golangci.yml`, `go.mod`.
- **Dependencies:** None.
- **Validation:** Full run passes on a fresh PR; runtime ≈ current runtime; no flaky additions.

### Task 15 — `.github/workflows/release.yml` — **NEW but DEFERRED (recommendation)**

- **Purpose:** Automated tagging/release builds.
- **Recommendation:** **Do not create this yet.** There are no tags, no release convention, and the repo is not yet public; automation now would codify a process that hasn't been exercised. Instead: cut the first tag (`v0.1.0`) manually with `gh release` once the repo is public, and create a GoReleaser-based workflow (build cross-platform binaries + checksums + embed UI via committed `dist` as the artifact source) **at the time of the second tag**, once the manual first release validates the artifact list. If the maintainer disagrees and wants it now, it should be a minimal `on: push: tags: ["v*"]` goreleaser job producing Linux/macOS/Windows binaries + attach assets. **Decision required: defer vs. create now.**
- **Reference:** `go.mod`, `orjanda-ui/package.json`, `ui_embed.go`.
- **Dependencies:** Tasks 1, 6 (version).
- **Validation (if created):** Tag push triggers a build; artifacts install and `orjanda version`/`serve` smoke-test works.

---

## P2 — Documentation (create only where no equivalent exists)

> The user-facing gaps: PRD/TAD are engineering specs. There is **no** quick-start, no architecture overview written for humans, no configuration reference, no "develop the framework" guide. The deleted `ORJANDA-IMPLEMENTATION-PLAN.md` was internal, not user-facing. So all four docs are **NEW**, but each must *complement* PRD/TAD, not restate them.

### Task 16 — `docs/getting-started.md` — **NEW**

- **Must contain:** Requirements (Go 1.26+); install the CLI; scaffold a project (`orjanda init`, `orjanda new document`); write a first `Document` with `oj` tags (`config/config.go`, `schema` tags); run migrations (`orjanda migrate diff/up`); `orjanda serve` (development is the default environment); browse the admin UI at `localhost:8080`; exercise REST + agent chat; 15-minute "hello world" flow; full pointer to PRD §37 (example developer workflow) and §38 (agent workflow). Uses the CLI scaffold, since `orjanda-app-hr-example` no longer exists.
- **Reference:** `cli/` (all commands), `config/config.go`, `schema/`, `orjanda.yaml`, PRD §37–38.
- **Dependencies:** Task 1 (README quick-start must agree with this), Task 14 (commands verified by CI).
- **Validation:** Every step verifiable by executing the commands; matches `cli/*_test.go` behavior.

### Task 17 — `docs/architecture.md` — **NEW**

- **Must contain:** High-level, prose-level tour: the Document → Registry → Document Engine → REST/RPC/UI/Agent-tools pipeline; modular monolith + single binary (embedded `dist`, `ui_embed.go`); the 11 extension points (TAD §9); permission path (single `perm.Engine` — PRD §25.1); request lifecycle (PRD §12.2); agent runtime (TAD §11); ASCII diagram of package layers per TAD §5.1. Explicitly links to PRD §9/TAD §5.1 for depth — no re-derivation.
- **Reference:** PRD §9, §12, §23–25; TAD §5.1, §9; `site.go`, `ui_embed.go`.
- **Dependencies:** Task 1.
- **Validation:** An engineer understands the data flow in ~10 minutes without reading the specs; the diagram matches actual package boundaries.

### Task 18 — `docs/configuration.md` — **NEW**

- **Must contain:** User-facing config reference derived from `config/config.go` and `orjanda.yaml`: `env` (development/production — ORJANDA_ENV), `server`, `database` (drivers, DSN, pool), `auth.jwt_secret` (required in production, ≥32 chars — `config.ValidateJWTSecret`), `llm` (providers openai/anthropic/openai_compatible, api_key via `ORJANDA_*` env, tool_calling/structured_output overrides), `llm.safety.max_bulk_operations`; env-var override table (ORJANDA_ENV, ORJANDA_AUTH_JWT_SECRET, ORJANDA_OPENAI_API_KEY, ORJANDA_ANTHROPIC_API_KEY, ORJANDA_ prefix + key→underscore rule); example `orjanda.yaml` (safe, env-referenced — reuse root `orjanda.yaml`). Reference TAD §1.3 for the authoritative schema.
- **Reference:** `config/config.go`, `orjanda.yaml`, TAD §1.3, PRD §15.1.
- **Dependencies:** Task 16 (config needed to get started).
- **Validation:** Every key in `config.Config` documented; env naming rule matches `config.Load`.

### Task 19 — `docs/development.md` — **NEW**

- **Must contain:** How to develop the *framework itself*: package layout (TAD §5.1); running the full test suite incl. integration (Docker + testcontainers, `integration` tag); UI development (`npm run dev`, codegen via `orjanda-codegen.mjs`, regenerate `src/generated`), committing `orjanda-ui/dist`; golden rules (TAD §1: errors, context, naming, one perm path, transactional audit); lint/format; writing a new dialect/extension point per TAD §9. **Deliberately disjoint from CONTRIBUTING.md** (which covers the contribution workflow) — this is the deep technical guide. Also note the deleted internal `ORJANDA-IMPLEMENTATION-PLAN.md` was internal; consider restoring a sanitized public version later (**decision**).
- **Reference:** `AGENTS.md` (source of the conventions), TAD §1/§5.1/§9, PRD §32, `.golangci.yml`, `orjanda-codegen.mjs`, `testing/`.
- **Dependencies:** Tasks 4 (no overlap), 14, 16, 17.
- **Validation:** A new maintainer/contributor can reproduce all CI checks locally and extend a subsystem.

---

## P2 — Project hygiene

### Task 20 — Internal/private/review-only files review — **REVIEW / CLEANUP** (no deletions during planning)

- **Purpose:** Ensure nothing internal or embarrassing ships publicly.
- **Findings & actions (each is a decision point):**
  1. **`AGENTS.md`** — agent-instruction file; currently claims the repo is "documentation-only, pre-implementation" (false) and references the deleted `docs/ORJANDA-IMPLEMENTATION-PLAN.md`. Action: either update it to reflect reality (it is genuinely useful as public AI-agent contribution guidance) or remove it before going public. **Decision: keep+update vs. remove.**
  2. **Deleted internal docs still in git history:** `docs/REVIEW-2026-08-12.md` (internal code review naming critical findings — e.g., the JWT-key finding, now fixed per `config.go`) and `docs/ORJANDA-IMPLEMENTATION-PLAN.md`. They are gone from the tree (good) but recoverable in history. Action: decide whether to (a) leave as-is (they're invisible to casual visitors), or (b) scrub history before publication (requires history rewrite + GitHub support request to purge cached objects — significant, weigh carefully). **Decision required.**
  3. **Placeholder commit in history:** `e62908a "Implement feature X to enhance user experience..."` — a bot/placeholder commit message. Cosmetic; same history-scrub decision as above.
  4. **`.vscode/settings.json`** — tracked despite `.gitignore` ignoring `.vscode/`. Action: remove from git tracking or drop the ignore rule (consistency fix).
  5. **Author identity in history:** git author email (`mohamed.mohamed1996318@gmail.com`) is embedded in all 67 commits — visible on GitHub. If the maintainer does not want a personal email public, that's a history-scrub consideration. **Decision required** (tied to the public-contact decisions below).
  6. **Keep as-is (safe):** `orjanda.yaml` (env-interpolated, no real secrets), `orjanda-codegen.mjs`, `ui_embed.go`, `site.go` (JWT secret now from config, not a literal — verified), `.gitignore` coverage of `.env`/local config.
- **Reference:** `git log --all`, `git show bcb9fd1 --stat`, `.gitignore`, `config/config.go`.
- **Dependencies:** Everything else (do this last, before publication).
- **Validation:** Final scan finds no secrets, no internal review content in the tree, no stale doc claims, no tracked files the `.gitignore` intends to exclude; decisions above are recorded and applied.

---

## Summary

### 1. Prioritized task list
| # | Path | Type | Priority |
|---|------|------|----------|
| 1 | `README.md` | UPDATE | P0 |
| 2 | `LICENSE` | NEW (approval) | P0 |
| 3 | `SECURITY.md` | NEW | P0 |
| 4 | `CONTRIBUTING.md` | NEW | P0 |
| 5 | `CODE_OF_CONDUCT.md` | NEW | P0 |
| 6 | `CHANGELOG.md` | NEW | P1 |
| 7 | `SUPPORT.md` | NEW | P1 |
| 8 | `GOVERNANCE.md` | NEW | P1 |
| 9 | `CODEOWNERS` | NEW | P1 |
| 10 | `.github/ISSUE_TEMPLATE/bug_report.yml` | NEW | P1 |
| 11 | `.github/ISSUE_TEMPLATE/feature_request.yml` | NEW | P1 |
| 12 | `.github/ISSUE_TEMPLATE/config.yml` | NEW | P1 |
| 13 | `.github/PULL_REQUEST_TEMPLATE.md` | NEW | P1 |
| 14 | `.github/workflows/ci.yml` | UPDATE | P1 |
| 15 | `.github/workflows/release.yml` | NEW (defer — approval) | P1 |
| 16 | `docs/getting-started.md` | NEW | P2 |
| 17 | `docs/architecture.md` | NEW | P2 |
| 18 | `docs/configuration.md` | NEW | P2 |
| 19 | `docs/development.md` | NEW | P2 |
| 20 | Internal/private file review | REVIEW/CLEANUP | P2 |

### 2. Files that already exist and can be reused
- `AGENTS.md` (conventions source — needs decision/update)
- `docs/ORJANDA-PRD.md`, `docs/ORJANDA-TAD.md` (spec sources for all docs)
- `.github/workflows/ci.yml` (near-complete; minor update)
- `.golangci.yml` (lint/format config)
- `.gitignore` (already hardened)
- `orjanda.yaml` (safe example config)
- `cli/`, `config/`, `schema/`, `site.go`, `ui_embed.go`, `orjanda-codegen.mjs`, `orjanda-ui/package.json` (ground truth for command/config/documentation accuracy)
- `git log` (feature inventory for CHANGELOG; commit-message conventions)

### 3. Files that must be newly created
`LICENSE`, `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `CHANGELOG.md`, `SUPPORT.md`, `GOVERNANCE.md`, `CODEOWNERS`, `.github/ISSUE_TEMPLATE/bug_report.yml`, `.github/ISSUE_TEMPLATE/feature_request.yml`, `.github/ISSUE_TEMPLATE/config.yml`, `.github/PULL_REQUEST_TEMPLATE.md`, `docs/getting-started.md`, `docs/architecture.md`, `docs/configuration.md`, `docs/development.md` (and optionally `.github/workflows/release.yml`).

### 4. Decisions requiring your approval
1. **License** — recommended **Apache-2.0** (alt: MIT, MPL-2.0) and the copyright holder line.
2. **Public contact info** — security email/address, Code of Conduct contact, support channels, maintainer handle for CODEOWNERS/GOVERNANCE. Placeholders are used everywhere until supplied; also whether your personal git-author email should be public.
3. **AGENTS.md** — keep (update to reflect reality) or remove before publication.
4. **Git-history scrub** — whether to purge the deleted internal review doc / plan doc / placeholder commit (history rewrite + GitHub support; vs. leaving history as-is).
5. **`release.yml`** — defer to the second tag (recommended) vs. create now.
6. **First version number** — propose `v0.1.0` for CHANGELOG/README (confirms the "v0.x pre-1.0" scheme).
7. **GitHub Discussions** — enable as the Q&A channel (referenced by SUPPORT.md/config.yml), or keep everything in Issues.

### 5. Recommended execution order (after approval)
1. Task 2 (LICENSE) + Task 3 (SECURITY.md) — resolve legal/contact decisions first.
2. Task 1 (README) — single biggest public artifact.
3. Tasks 4–5 (CONTRIBUTING, CODE_OF_CONDUCT).
4. Tasks 10–13 (issue/PR templates), then 6–9 (CHANGELOG, SUPPORT, GOVERNANCE, CODEOWNERS).
5. Task 14 (CI hardening); Task 15 only if you choose to create it.
6. Tasks 16–19 (docs).
7. Task 20 (hygiene) — last, including the history/AGENTS.md decisions, immediately before publication.
