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
