---
page_title: "Setting up integrations by type"
subcategory: ""
description: |-
  Runnable examples for setting up common integration types — AWS (account and organization
  scans, S3, RDS, DynamoDB), Google Cloud (organization scan, Cloud Storage, BigQuery,
  Bigtable), Snowflake, Salesforce, and third-party API-key vendors — with the Relyance
  provider.
---

# Setting up integrations by type

Every integration is the same `relyance_integration_connection` resource. Only three things
change per vendor:

- **`vendor`** — the vendor key (e.g. `aws_s3`, `gcloud`, `snowflake`).
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

JSON-shaped fields (e.g. `organization_config`, `crawler_rules`, `config_options`) take a
`jsonencode({ ... })` value; the expected shape is described in the vendor's field schema.

---

## AWS

### Account scan

Discover assets across a single AWS account via an assumed IAM role. `external_id` is the
only secret.

```terraform
# AWS account scan: discover assets across a single AWS account via an assumed
# IAM role. For scanning a whole AWS Organization, see aws_org_scan.tf.
resource "relyance_integration_connection" "aws_account" {
  vendor = "aws"
  name   = "AWS production account"

  auth = {
    method = "iam-role-direct"
    params = {
      role_arn    = "arn:aws:iam::123456789012:role/RelyanceReadOnlyRole"
      region_name = "us-east-1"
    }
    secrets_wo         = { external_id = var.aws_external_id }
    secrets_wo_version = 1
  }

  scans = { "assets-discovery" = { enabled = true } }
}
```

### Organization scan

Discovers accounts across a whole AWS Organization via a delegated Resource Explorer view
and org-reader roles.

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

### S3 — Inventory scan

Reads pre-generated S3 Inventory reports via an assumed IAM role (cheaper/faster than a
direct bucket crawl).

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

### RDS

Classifies data in relational databases via an assumed IAM role. `crawler_rules` scopes
which databases/tables are crawled; the `data-inspection` scan takes tuning fields such as
`sampling_percent`.

```terraform
# Amazon RDS: classify data in relational databases via an assumed IAM role.
# crawler_rules scopes which databases/tables are crawled.
resource "relyance_integration_connection" "rds" {
  vendor = "aws_rds"
  name   = "Production RDS"

  auth = {
    method = "iam-role-based"
    params = {
      role_arn = "arn:aws:iam::123456789012:role/RelyanceRDSScanRole"
      region   = "us-east-1"
      crawler_rules = jsonencode({
        allow_list = ["orders_db.*"]
        block_list = [""]
      })
    }
    secrets_wo         = { external_id = var.rds_external_id }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection" = {
      enabled = true
      # Scan-level tuning fields come from the catalog's kind schema.
      fields = { sampling_percent = "10" }
    }
  }
}
```

### DynamoDB

Classifies data in DynamoDB tables via an assumed IAM role; `tables` lists the tables to
scan.

```terraform
# Amazon DynamoDB: classify data in DynamoDB tables via an assumed IAM role.
resource "relyance_integration_connection" "dynamodb" {
  vendor = "aws_dynamodb"
  name   = "Production DynamoDB"

  auth = {
    method = "iam-role-direct"
    params = {
      account_id = "123456789012"
      role_name  = "RelyanceDynamoDBScanRole"
      region     = "us-east-1"
      tables     = jsonencode(["customers", "orders"])
    }
    secrets_wo         = { external_id = var.dynamodb_external_id }
    secrets_wo_version = 1
  }

  scans = { "data-inspection" = { enabled = true } }
}
```

---

## Google Cloud

### Organization scan

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

### Cloud Storage — Inventory Reports

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

### BigQuery

Classifies data in BigQuery datasets via a service account; `config_options` allow/deny
lists scope datasets and tables.

```terraform
# Google BigQuery: classify data in datasets via a service account. For this
# method the service-account email and scan project id are secret fields.
resource "relyance_integration_connection" "bigquery" {
  vendor = "gcloud_bigquery"
  name   = "Analytics BigQuery"

  auth = {
    method = "service-account"
    params = {
      config_options = jsonencode({
        allow_list_datasets = []
        allow_list_tables   = []
        deny_list_datasets  = ["staging_scratch"]
      })
    }
    secrets_wo = {
      service_account_email = var.bq_service_account_email
      project_id_scan       = var.bq_project_id
    }
    secrets_wo_version = 1
  }

  scans = { "data-inspection" = { enabled = true } }
}
```

### Bigtable

Classifies data in Bigtable instances via a service account; `instance_config` scopes
instances and tables.

```terraform
# Google Cloud Bigtable: classify data in Bigtable instances via a service account.
resource "relyance_integration_connection" "bigtable" {
  vendor = "gcloud_bigtable"
  name   = "Production Bigtable"

  auth = {
    method = "service-account"
    params = {
      instance_config = jsonencode([
        {
          instance_id  = "prod-instance"
          allow_tables = [""]
          block_tables = [""]
        }
      ])
    }
    secrets_wo = {
      service_account_email = var.bigtable_service_account_email
      project_id_scan       = var.bigtable_project_id
    }
    secrets_wo_version = 1
  }

  scans = { "data-inspection" = { enabled = true } }
}
```

---

## Snowflake

Key-pair authentication is the recommended method for automation: asymmetric, rotatable, and
no shared password. `config_options` allow/deny lists scope databases.

```terraform
# Snowflake via key-pair authentication (recommended for automation: asymmetric,
# rotatable, no shared password).
resource "relyance_integration_connection" "snowflake" {
  vendor = "snowflake"
  name   = "Snowflake production"

  auth = {
    method = "key-pair"
    params = {
      account       = "myorg-prod"
      user          = "RELYANCE_SVC"
      role          = "RELYANCE_SCANNER"
      use_warehouse = "RELYANCE_WH"
      config_options = jsonencode({
        allow_list = []
        deny_list  = ["SNOWFLAKE_SAMPLE_DATA.*"]
      })
      data_storage_location = "us"
    }
    secrets_wo = {
      # PEM-encoded private key for the Snowflake user.
      private_key = var.snowflake_private_key
      # The catalog marks skip_views as a secret field, so it belongs here.
      skip_views = "false"
    }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection"  = { enabled = true }
    "assets-discovery" = { enabled = true }
  }
}
```

---

## Salesforce — OAuth client credentials (machine-to-machine)

A Salesforce Connected App with the client-credentials flow enabled needs no browser
authorization, so the whole connection is manageable from Terraform. Both `client_id` and
`client_secret` are secret for this method.

```terraform
# Salesforce via OAuth client credentials (machine-to-machine): a Connected App
# with the client-credentials flow enabled -- no browser authorization needed,
# so the whole connection is manageable from Terraform.
resource "relyance_integration_connection" "salesforce" {
  vendor = "salesforce"
  name   = "Salesforce production"

  auth = {
    method = "oauth-client-credentials"
    params = {
      domain                    = "your-org.my.salesforce.com"
      maximum_number_of_records = "5000"
      data_storage_location     = "us"
    }
    secrets_wo = {
      # Both are secret for this method per the vendor catalog.
      client_id     = var.salesforce_client_id
      client_secret = var.salesforce_client_secret
    }
    secrets_wo_version = 1
  }

  scans = { "data-inspection" = { enabled = true } }
}
```

Vendors whose only auth is a browser OAuth grant can't be fully created from Terraform —
authorize them in the Relyance app once, then `import` the connection (or read it with the
`relyance_integration_connection` data source) to manage everything else as code.

---

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
