terraform {
  required_providers {
    relyance = {
      source  = "relyance/relyance"
      version = "~> 1.0"
    }
  }
}

provider "relyance" {
  client_id     = var.relyance_client_id
  client_secret = var.relyance_client_secret
}

# Read the vendor's auth methods and field schema from the live catalog —
# each field reports whether it is secret.
data "relyance_integration_vendor" "s3" {
  vendor = "aws_s3"
}

resource "relyance_integration_connection" "s3" {
  vendor = "aws_s3"
  name   = "Production data lake"

  auth = {
    method = "iam-role-s3-inventory"
    params = {
      account_id = "123456789012"
      role_name  = "RelyanceS3InventoryRole"
      region     = "us-east-1"
    }
    # Secret fields are write-only: sent to Relyance, never stored in state.
    secrets_wo         = { external_id = var.s3_external_id }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection" = { enabled = true }
  }
}
