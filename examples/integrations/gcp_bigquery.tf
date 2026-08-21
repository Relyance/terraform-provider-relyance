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
