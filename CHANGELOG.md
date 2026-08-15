# Changelog

Releases are automated with [semantic-release](https://semantic-release.gitbook.io/) from the
Conventional Commit history on `main`. The canonical, per-version changelog lives in the
**[GitHub Releases](https://github.com/Relyance/terraform-provider-relyance/releases)** (also
surfaced on the Terraform Registry); this file is not maintained per-release.

## Initial release

- Resource `relyance_integration_connection` — manage a vendor integration connection: name,
  authentication (write-only secret arguments with rotation-by-version), scan capabilities,
  business nodes, and lifecycle, including import.
- Data sources `relyance_integration_vendor`, `relyance_integration_connection`, and
  `relyance_business_nodes`.
- Tenant OAuth-client authentication via `client_credentials` (client secret or
  `private_key_jwt`), with the API edge performing the per-route audience exchange.
