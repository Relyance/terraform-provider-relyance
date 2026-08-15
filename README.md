# terraform-provider-relyance

[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](./LICENSE)

Terraform provider for Relyance tenant configuration. v1 manages integration
connections (create, configure auth and scan kinds, import, delete) against
integrations-api, authenticating as a tenant OAuth client (`client_credentials`
with a client secret or `private_key_jwt`).

> Status: pre-release. Requires Terraform **>= 1.11** (write-only secret arguments).

## Using the provider

```hcl
terraform {
  required_providers {
    relyance = {
      source  = "relyance/relyance"
      version = "~> 0.1"
    }
  }
}

provider "relyance" {
  # endpoint defaults to the production Relyance API; set it only to target a
  # non-production environment (env: RELYANCE_ENDPOINT).
  client_id = var.relyance_client_id
  # one of client_secret / jwk_json (private_key_jwt):
  client_secret = var.relyance_client_secret
}
```

Provider credentials may also be supplied via `RELYANCE_ENDPOINT`, `RELYANCE_CLIENT_ID`,
and `RELYANCE_CLIENT_SECRET` / `RELYANCE_JWK_JSON`.

## What it manages

- `relyance_integration_connection` (resource) — a vendor integration connection: name,
  auth (with write-only secrets), scan capabilities, business nodes, and lifecycle.
- `relyance_integration_vendor` (data source) — a vendor's authentication methods and field
  schemas, including which fields are secret.
- `relyance_integration_connection` (data source) — read a connection Terraform doesn't own
  (e.g. a browser-authorized OAuth vendor) and gate on its live status.
- `relyance_business_nodes` (data source) — look up business nodes by name.

## Documentation

Full reference docs are under [`docs/`](./docs) (generated with `tfplugindocs`) and rendered
on the Terraform Registry. Runnable examples are in [`examples/`](./examples).

## Contributing & security

- Development, testing, and release process: [DEVELOPMENT.md](./DEVELOPMENT.md)
- Security model, secret handling, and reporting: [SECURITY.md](./SECURITY.md)

## License

[MPL-2.0](./LICENSE).
