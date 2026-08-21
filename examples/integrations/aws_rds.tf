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
