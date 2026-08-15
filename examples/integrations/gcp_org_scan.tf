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
