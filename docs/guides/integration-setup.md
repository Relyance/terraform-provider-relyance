---
page_title: "Setting up integrations by type"
subcategory: ""
description: |-
  Runnable examples for setting up common integration types — AWS S3 Inventory, AWS
  Organization scan, GCP, and third-party API-key vendors — with the Relyance provider.
---

# Setting up integrations by type

Every integration is the same `relyance_integration_connection` resource. Only three things
change per vendor:

- **`vendor`** — the vendor key (e.g. `aws_s3`, `gcloud`, `atlassian_jira`).
- **`auth.method`** — the authentication-method slug for that vendor.
- **`auth.params` / `auth.secrets_wo`** — the method's fields, split by whether they are secret.

Non-secret fields go in `auth.params`; secret fields go in `auth.secrets_wo`, which is
**write-only** (never stored in Terraform state). Rotate a secret by bumping
`auth.secrets_wo_version`.

## Discovering a vendor's fields

Don't guess field names — read them from the catalog. Each field reports `is_secret`, which
tells you whether it belongs in `params` or `secrets_wo`:

```terraform
data "relyance_integration_vendor" "v" {
  vendor = "aws_s3"
}

output "methods" {
  value = data.relyance_integration_vendor.v.auth_methods
}
```

JSON-shaped fields (e.g. `organization_config`, `json_config`, `bucket_config`) take a
`jsonencode({ ... })` value; the expected shape is described in the vendor's field schema.

## AWS S3 — S3 Inventory scan

Reads pre-generated S3 Inventory reports via an assumed IAM role (cheaper/faster than a direct
bucket crawl). `external_id` is the only secret.

```terraform
# AWS S3 — S3 Inventory scan
#
# Uses the "iam-role-s3-inventory" auth method: Relyance reads pre-generated S3
# Inventory reports (cheaper/faster than a direct bucket crawl) via an assumed IAM role.
# Discover a vendor's methods and field schema (and which fields are secret) with:
#   data "relyance_integration_vendor" "aws_s3" { vendor = "aws_s3" }
resource "relyance_integration_connection" "s3_inventory" {
  vendor = "aws_s3"
  name   = "Prod data-lake (S3 Inventory)"

  auth = {
    method = "iam-role-s3-inventory"
    params = {
      account_id              = "123456789012"
      role_name               = "RelyanceS3InventoryRole"
      region                  = "us-east-1"
      bucket_prefixes         = jsonencode([]) # empty = all buckets; or ["logs/", "data/"]
      max_concurrent_requests = "10"
    }
    # external_id is a write-only secret — never stored in state. Bump secrets_wo_version to rotate.
    secrets_wo         = { external_id = var.s3_external_id }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection" = { enabled = true }
  }
}

variable "s3_external_id" {
  type      = string
  sensitive = true
}
```

## AWS Organization scan

Discovers accounts across an AWS Organization via a delegated Resource Explorer view and
org-reader roles. Use the `aws` vendor's `iam-role-direct` method instead for a single account.

```terraform
# AWS Organization scan
#
# The "aws" vendor with "iam-role-organization-scan" discovers accounts across an AWS
# Organization (via a delegated Resource Explorer view + org-reader roles) rather than a
# single account. Use "iam-role-direct" instead for a single-account setup.
resource "relyance_integration_connection" "aws_org" {
  vendor = "aws"
  name   = "AWS Organization"

  auth = {
    method = "iam-role-organization-scan"
    params = {
      org_reader_role_arn                         = "arn:aws:iam::111111111111:role/RelyanceOrgReader"
      delegated_resource_explorer_view_arn        = "arn:aws:resource-explorer-2:us-east-1:111111111111:view/relyance/abcd"
      delegated_resource_explorer_reader_role_arn = "arn:aws:iam::111111111111:role/RelyanceResourceExplorerReader"
      assumable_account_reader_role_name          = "RelyanceAccountReader"
      # organization_config is a JSON blob; see the relyance_integration_vendor data source
      # for the exact shape (e.g. which OUs/accounts to include or exclude).
      organization_config = jsonencode({ include_all_accounts = true })
    }
    secrets_wo         = { external_id = var.aws_org_external_id }
    secrets_wo_version = 1
  }

  scans = {
    "vendor-discovery" = { enabled = true }
  }
}

variable "aws_org_external_id" {
  type      = string
  sensitive = true
}
```

## GCP Organization scan

Scans a whole GCP Organization via a service account with org-level read. This method has no
secret fields — the service account is referenced by email, so omit `secrets_wo` entirely.

```terraform
# GCP Organization scan
#
# The "gcloud" vendor with "service-account-organization-scan" scans a whole GCP
# Organization via a service account granted org-level read. All fields here are
# non-secret (the service account is referenced by email, not by key material) so they
# go in params; there are no write-only secrets for this method.
resource "relyance_integration_connection" "gcp_org" {
  vendor = "gcloud"
  name   = "GCP Organization"

  auth = {
    method = "service-account-organization-scan"
    params = {
      organization_id       = "123456789012"
      service_account_email = "relyance-scanner@my-project.iam.gserviceaccount.com"
      # organization_config is a JSON blob; see the relyance_integration_vendor data source.
      organization_config = jsonencode({ include_all_projects = true })
    }
    # No secrets for this method — omit secrets_wo entirely.
  }

  scans = {
    "vendor-discovery" = { enabled = true }
  }
}
```

## GCP Cloud Storage — Inventory Reports

Reads GCS Storage Inventory Reports. Here the service-account email and scan project id are
secret, so they go in `secrets_wo`.

```terraform
# GCP Cloud Storage — Inventory Reports scan
#
# The "gcloud_storage" vendor with "service-account-inventory-reports" reads GCS
# Storage Inventory Reports via a service account. Here the service account email and
# the scan project id are marked SECRET in the catalog, so they belong in secrets_wo
# (write-only); bucket_config / max_concurrent_requests are plain params.
resource "relyance_integration_connection" "gcs_inventory" {
  vendor = "gcloud_storage"
  name   = "GCS Inventory Reports"

  auth = {
    method = "service-account-inventory-reports"
    params = {
      # bucket_config is a JSON blob; see the relyance_integration_vendor data source.
      bucket_config           = jsonencode({ buckets = ["gs://my-inventory-bucket"] })
      max_concurrent_requests = "10"
    }
    secrets_wo = {
      service_account_email = var.gcs_service_account_email
      project_id_scan       = var.gcs_project_id
    }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection" = { enabled = true }
  }
}

variable "gcs_service_account_email" {
  type      = string
  sensitive = true
}
variable "gcs_project_id" {
  type      = string
  sensitive = true
}
```

## Third-party SaaS vendor via API key

Many SaaS vendors authenticate with an API key/token. For Jira's `api-key` method both `ORG_ID`
and `API_KEY` are secret.

```terraform
# Third-party SaaS vendor via API key — Atlassian Jira
#
# Many SaaS vendors authenticate with an API key/token. For Jira's "api-key" method,
# both ORG_ID and API_KEY are secret (write-only); data_storage_location is a plain param.
# (Jira also offers "oauth" and "api-token" methods — see the vendor data source.)
resource "relyance_integration_connection" "jira" {
  vendor = "atlassian_jira"
  name   = "Jira (API key)"

  auth = {
    method = "api-key"
    params = {
      data_storage_location = "us"
    }
    secrets_wo = {
      ORG_ID  = var.jira_org_id
      API_KEY = var.jira_api_key
    }
    secrets_wo_version = 1
  }

  scans = {
    "vendor-discovery" = { enabled = true }
  }
}

variable "jira_org_id" {
  type      = string
  sensitive = true
}
variable "jira_api_key" {
  type      = string
  sensitive = true
}
```

## Custom third-party API via API key

`relyance_customapiimport` is the generic connector for an API that isn't a first-class vendor;
its `api-key` method sends a single secret key plus a `json_config` describing the endpoints.

```terraform
# Custom / arbitrary third-party API via API key
#
# "relyance_customapiimport" is the generic connector for pulling from a third-party API
# that isn't a first-class vendor. The "api-key" method sends a single bearer/API key
# (secret) plus a json_config describing the endpoints/response mapping. It also supports
# oauth-client-credentials, username-password, key-pair, etc. — see the vendor data source.
resource "relyance_integration_connection" "custom_api" {
  vendor = "relyance_customapiimport"
  name   = "Acme internal API (API key)"

  auth = {
    method = "api-key"
    params = {
      auth_method         = "header" # how the key is sent, e.g. header/bearer
      api_response_format = "json"
      target_vendor       = "acme"
      internal_service    = "false"
      # json_config is a JSON blob describing endpoints + field mapping; see the vendor data source.
      json_config           = jsonencode({ base_url = "https://api.acme.example", endpoints = [] })
      data_storage_location = "us"
    }
    secrets_wo         = { api_key = var.acme_api_key }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection" = { enabled = true }
  }
}

variable "acme_api_key" {
  type      = string
  sensitive = true
}
```
