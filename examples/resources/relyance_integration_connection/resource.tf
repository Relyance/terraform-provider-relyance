data "relyance_integration_vendor" "s3" {
  vendor = "aws_s3"
}

data "relyance_business_nodes" "all" {}

resource "relyance_integration_connection" "s3" {
  vendor = "aws_s3"
  name   = "Data-lake S3"

  scan_from = "2026-08-01T00:00:00Z"

  # Reference business nodes by name, not record id:
  business_node_ids = [data.relyance_business_nodes.all.by_name["Engineering"]]

  auth = {
    method = "iam-role-direct"
    params = {
      account_id = "123456789012"
      role_name  = "S3ScanRole"
      region     = "us-east-2"
    }
    # Secret fields (is_secret in the vendor data source) are write-only:
    # never stored in state. Bump secrets_wo_version to re-send after rotation.
    secrets_wo         = { external_id = var.s3_external_id }
    secrets_wo_version = 1
  }

  # Scan capabilities (see relyance_integration_vendor for available scans):
  scans = {
    data-inspection  = { enabled = true, fields = { sampling_percent = "10" } }
    assets-discovery = { enabled = true }
  }
}
