# Orjanda Governance

This document describes how Orjanda is governed and how decisions are made. It
is intentionally lightweight: the project starts with a single maintainer and a
defined path to grow that role as the community does.

## Maintainer model

Orjanda currently operates under a **single-maintainer (benevolent-dictator)
model** with a defined growth path.

- **Maintainer:** Mohamed Kamal ([@mookamal](https://github.com/mookamal))
- The maintainer holds final authority over the repository, releases, and
  scope, and is responsible for:
  - reviewing and merging (or closing) pull requests,
  - responding to issues and security reports,
  - cutting version tags and driving releases,
  - enforcing the [Code of Conduct](CODE_OF_CONDUCT.md),
  - keeping the specifications (`docs/ORJANDA-PRD.md`, `docs/ORJANDA-TAD.md`)
    in sync with the code.

Decisions are made with the goal of keeping Orjanda focused on its documented
thesis (PRD §1–§9). The single-maintainer model is a starting point, not a
ceiling — see [Adding maintainers](#adding-maintainers).

## How decisions are made

### Contributions and small changes

- Anyone can contribute via the standard flow in
  [CONTRIBUTING.md](CONTRIBUTING.md): fork, feature branch, PR against `main`.
- The maintainer reviews and merges PRs. **Lazy consensus** applies to minor
  changes (bug fixes, docs, tests, refactors with no behavior change): if a PR
  is correct, passes CI, and has no objections within a reasonable window, it
  is merged. Objections are resolved by discussion, not voting.
- Behavior changes, permission/security-relevant changes, and agent-runtime
  changes receive extra scrutiny regardless of size.

### Architectural and scope decisions

The PRD and TAD are the **source of truth** for interfaces, packages, and
behavior — no new architecture gets invented in code first.

- Any change that alters an interface, package, field, or documented behavior
  **must first amend the specifications**: update `docs/ORJANDA-PRD.md` (why)
  and/or `docs/ORJANDA-TAD.md` (exact shape), then implement against the
  amended spec.
- Larger changes follow an RFC-style process: the proposal is opened as an
  issue (or discussion), the maintainer assesses it against the project thesis
  and scope boundaries (PRD §5 Non-Goals, §44.3 MVP non-scope), and — if
  accepted — it is added to the specifications before implementation.
- Out-of-scope requests (plugins, MCP, MySQL, multi-tenancy activation, and
  the rest of PRD §5/§44.3) are closed, not half-adopted.

### Conflicts

Disagreements are resolved by discussion first, escalating to the maintainer's
decision. When the maintainer rules against a proposal, the reasons are
explained in the thread. There is no formal voting body at this stage.

## Adding maintainers

As contributions grow, the maintainer may invite trusted, consistent
contributors to become co-maintainers. Criteria:

- A history of high-quality, spec-aligned contributions and reliable review
  feedback,
- Demonstrated judgment about Orjanda's scope and conventions,
- Agreement to follow this governance, the Code of Conduct, and the security
  policy.

Co-maintainers are added by the maintainer (and announced in the release notes
or README). A contributor who has had their invitation accepted gains merge
rights and shares the responsibilities above. The maintainer remains the final
arbiter until a formal transition is made.

## Release process

- Releases follow [Semantic Versioning](https://semver.org/) as noted in
  [CHANGELOG.md](CHANGELOG.md): pre-1.0, `0.x minor` = breaking.
- The maintainer cuts version tags (proposed first tag: `0.1.0`), updates the
  CHANGELOG, and publishes release assets.
- Release mechanics are kept manual until the release process has been
  exercised at least once (see the release preparation plan).

## Version control of this document

This governance can itself be amended, but changes must be made in a dedicated
PR, reviewed with the same care as a spec change, and never silently merged as
part of an unrelated change.
