# Integration examples

Runnable examples for common integration types. Every integration uses the same
`relyance_integration_connection` resource — only `vendor`, `auth.method`, and the
`auth.params` / `auth.secrets_wo` fields differ per vendor.

| File | Vendor | Auth method | What it shows |
|------|--------|-------------|---------------|
| [aws_s3_inventory.tf](./aws_s3_inventory.tf) | `aws_s3` | `iam-role-s3-inventory` | AWS S3 via S3 Inventory reports (assumed IAM role) |
| [aws_org_scan.tf](./aws_org_scan.tf) | `aws` | `iam-role-organization-scan` | AWS Organization-wide discovery |
| [gcp_org_scan.tf](./gcp_org_scan.tf) | `gcloud` | `service-account-organization-scan` | GCP Organization-wide scan (no secrets) |
| [gcp_storage_inventory.tf](./gcp_storage_inventory.tf) | `gcloud_storage` | `service-account-inventory-reports` | GCS Inventory Reports scan |
| [vendor_api_key_jira.tf](./vendor_api_key_jira.tf) | `atlassian_jira` | `api-key` | Third-party SaaS via API key |
| [custom_api_key.tf](./custom_api_key.tf) | `relyance_customapiimport` | `api-key` | Arbitrary third-party API via API key |

## The pattern

```hcl
resource "relyance_integration_connection" "example" {
  vendor = "<vendor_key>"          # e.g. aws_s3, gcloud, atlassian_jira
  name   = "Human-readable name"

  auth = {
    method     = "<auth_method_slug>"   # e.g. iam-role-s3-inventory
    params     = { ... }                # NON-secret fields
    secrets_wo = { ... }                # SECRET fields — write-only, never stored in state
    secrets_wo_version = 1              # bump to rotate secrets (write-only values can't be diffed)
  }

  scans = {
    "data-inspection" = { enabled = true }   # scan capabilities to enable (slugs)
  }
}
```

## Discovering a vendor's fields

Don't guess field names — read them from the catalog. Each field reports whether it is
secret (`is_secret`), which tells you whether it goes in `params` or `secrets_wo`:

```hcl
data "relyance_integration_vendor" "v" {
  vendor = "aws_s3"
}

output "methods" { value = data.relyance_integration_vendor.v.auth_methods }
```

Secret fields → `auth.secrets_wo` (write-only, never persisted; rotate via `secrets_wo_version`).
Non-secret fields → `auth.params`. JSON-shaped fields (e.g. `organization_config`, `json_config`,
`bucket_config`) take a `jsonencode({...})` value — the exact shape is described in the vendor's
field schema.
