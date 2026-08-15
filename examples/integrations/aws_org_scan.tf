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
