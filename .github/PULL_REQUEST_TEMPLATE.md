## Description

<!-- What does this change do, and why? Link related issues (e.g. `Closes #123`). -->

## Checklist

- [ ] Tests added or updated for the behavior change (unit tests under `internal/...`,
      and an acceptance test if it affects resource/data-source CRUD behavior)
- [ ] `go test ./...` passes locally
- [ ] `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run` is clean
- [ ] Docs regenerated (`./scripts/gendocs.sh`) and committed, if schema or `examples/` changed
- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) (e.g.
      `feat: ...`, `fix: ...`, `docs: ...`)
- [ ] No secrets, credentials, or real tenant data committed (config, tests, or docs)
