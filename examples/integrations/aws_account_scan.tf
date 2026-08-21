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
