# Development

## Requirements

- Go (see `go.mod` for the version)
- Terraform ≥ 1.11 (for write-only argument support in examples/acceptance tests)

## Build, vet, test

```sh
go build ./...
go vet ./...
go test ./...          # unit tests (fast, no network)
```

## Running against a local build (dev overrides)

Build the binary and point Terraform at it so `terraform plan/apply` use your local build
without a registry install:

```sh
go build -o "$(go env GOBIN)/terraform-provider-relyance" ./cmd/terraform-provider-relyance
```

Create a `dev.tfrc`:

```hcl
provider_installation {
  dev_overrides {
    "relyance/relyance" = "/path/to/dir/containing/the/binary"
  }
  direct {}
}
```

Then `export TF_CLI_CONFIG_FILE=$PWD/dev.tfrc` and run Terraform normally (skip
`terraform init`; dev overrides bypass provider installation).

Provider configuration comes from env vars (see the provider docs): `RELYANCE_ENDPOINT`,
`RELYANCE_CLIENT_ID`, and `RELYANCE_CLIENT_SECRET` **or** `RELYANCE_JWK_JSON`.

## Documentation

Docs under `docs/` are generated from the schema + `examples/` with `tfplugindocs`:

```sh
./scripts/gendocs.sh
```

CI fails if `docs/` is out of date, so run this and commit whenever the schema or examples
change.

## Acceptance tests

Acceptance tests run real CRUD against a live environment and are gated behind `TF_ACC`:

```sh
export TF_ACC=1
export RELYANCE_ENDPOINT=https://relyance.example.com  # your non-production endpoint
export RELYANCE_CLIENT_ID=... RELYANCE_CLIENT_SECRET=...
go test ./... -run TestAcc -v -timeout 30m
```

They create and destroy throwaway resources against the configured tenant — point them at a
non-production tenant.

## Releasing

Releases are automated — there is no manual tagging. On merge to `main`, the `release`
workflow runs **semantic-release**, which reads the Conventional Commit history, computes the
next semver version, and creates the `vX.Y.Z` tag + GitHub Release. **GoReleaser** then builds,
checksums, and **GPG-signs** the artifacts and appends them to that release, which the Terraform
Registry ingests. A merge with no releasable commits produces no release.

Because the version is derived from commit messages, PR titles/commits must follow
[Conventional Commits](https://www.conventionalcommits.org/) (enforced by the `Semantic PR
Title` check). Breaking changes require a `!` or a `BREAKING CHANGE:` footer.

Required repo secrets: `GPG_PRIVATE_KEY` (ASCII-armored private key) and `PASSPHRASE`. The
corresponding public key must be uploaded to the provider's Terraform Registry namespace.
