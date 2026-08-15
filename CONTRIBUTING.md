# Contributing

Thanks for your interest in improving `terraform-provider-relyance`. This document covers the
mechanics of sending a change; for local build/test/release workflows see
[DEVELOPMENT.md](./DEVELOPMENT.md), and for reporting vulnerabilities see
[SECURITY.md](./SECURITY.md) (please do not file public issues for security reports).

## Prerequisites

- Go — the version pinned in [`go.mod`](./go.mod).
- Terraform **>= 1.11** (required for write-only argument support used by
  `relyance_integration_connection`'s secret fields).

## Before you open a PR

Run the same checks CI runs:

```sh
go build ./...
go vet ./...
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run
```

If you touched the provider/resource/data-source **schema** or anything under `examples/`,
regenerate the docs and commit the result:

```sh
./scripts/gendocs.sh
```

CI's `docs` job runs the same generator and fails the build on any drift between `docs/` and
what the schema/examples produce — regenerate locally rather than hand-editing files under
`docs/`.

### Acceptance tests

Acceptance tests exercise real CRUD against a live Relyance environment and are gated behind
`TF_ACC` so they never run by accident (including in the default CI job):

```sh
export TF_ACC=1
export RELYANCE_ENDPOINT=https://relyance.example.com  # your non-production endpoint
export RELYANCE_CLIENT_ID=... RELYANCE_CLIENT_SECRET=...
go test ./... -run TestAcc -v -timeout 30m
```

Point them at a non-production tenant — they create and destroy throwaway resources. You don't
need to run these for most PRs; unit tests (`go test ./...`, no `TF_ACC`) are what CI enforces
on every push. If your change affects resource/data-source behavior, consider adding or
updating an acceptance test in the relevant `internal/provider/...` package alongside it.

## Commit messages and PR titles

This repo uses [Conventional Commits](https://www.conventionalcommits.org/) — lowercase,
single-line subject, e.g. `fix(security): enforce https endpoint`, `feat: add scan diffing`,
`docs: regenerate provider reference`. Use `!` (e.g. `feat!:`) or a `BREAKING CHANGE:` footer
for breaking changes. Keep commits logical and incremental rather than one large diff — it
makes review and `git bisect` easier.

## Opening a pull request

1. Fork the repo and branch from `main`.
2. Make your change, keeping commits focused and following the conventions above.
3. Ensure `go build ./...`, `go vet ./...`, `go test ./...`, and golangci-lint are clean, and
   that `docs/` is regenerated if you changed schema or examples.
4. Open the PR against `main` with a Conventional Commits–style title (CI/release tooling
   depends on it) and a description of the change and why it's needed.
5. CI (`test` workflow) runs build, vet, unit tests, and the docs-drift check on every PR — all
   must pass before merge.
6. Respond to review feedback; maintainers will merge once the PR is approved and green.

## Reporting bugs and requesting features

Please use the issue templates (bug report / feature request) rather than an unstructured
issue — they capture the details (provider/Terraform version, config, logs) needed to
reproduce or evaluate a request. Never include real credentials or tenant secrets in an issue;
redact config snippets and logs before pasting them.
