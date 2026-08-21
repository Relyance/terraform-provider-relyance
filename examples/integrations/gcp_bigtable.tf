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
