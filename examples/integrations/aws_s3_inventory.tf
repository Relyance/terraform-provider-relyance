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
