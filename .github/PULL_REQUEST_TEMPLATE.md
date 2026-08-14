<!--
Thank you for contributing to Orjanda. Please fill out this template — it helps
maintainers review your change quickly. Read CONTRIBUTING.md first if you have
not already.
-->

## Summary

<!-- What does this PR do, and why? Keep it concise. Reference PRD/TAD sections
instead of restating them, e.g. "see TAD §8.1 step 3". -->

## Type of change

- [ ] `feat` — new feature
- [ ] `fix` — bug fix
- [ ] `docs` — documentation only
- [ ] `refactor` — no behavior change
- [ ] `chore` — tooling, CI, housekeeping

## Related issue(s)

<!-- Link to issues this PR closes or relates to, e.g. Closes #12. -->

## Testing performed

<!-- Run the relevant commands from CONTRIBUTING.md and paste the result. -->

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
```

- [ ] `golangci-lint run` passes
- [ ] `gofmt` clean (`gofmt -l .` reports nothing)
- [ ] Integration lane (`go test -tags integration -count=1 ./testing/...`) passes if relevant
- [ ] UI lane (`npm run typecheck && npm test && npm run build` in `orjanda-ui/`) passes if the UI was touched
- [ ] Tests added for changed behavior

## Checklist

- [ ] I read CONTRIBUTING.md and followed the setup, test, and workflow guidance
- [ ] My change follows the architecture guidelines (package DAG per TAD §5.1, extension points per TAD §9, code-first schemas per PRD §8.4)
- [ ] No parallel permission paths: every read/write goes through `perm.Engine` (PRD §25.1)
- [ ] Errors use the framework error model (`errors.Error` with a defined `ErrorCode`, TAD §1.1) — no new error types
- [ ] PRD/TAD section numbers cited where behavior depends on a spec
- [ ] `CHANGELOG.md` updated under `[Unreleased]`
- [ ] No secrets, credentials, or personal data included
- [ ] Committed `orjanda-ui/dist/` regenerated if the UI changed
