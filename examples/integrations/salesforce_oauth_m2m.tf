# Salesforce via OAuth client credentials (machine-to-machine): a Connected App
# with the client-credentials flow enabled -- no browser authorization needed,
# so the whole connection is manageable from Terraform.
resource "relyance_integration_connection" "salesforce" {
  vendor = "salesforce"
  name   = "Salesforce production"

  auth = {
    method = "oauth-client-credentials"
    params = {
      domain                    = "your-org.my.salesforce.com"
      maximum_number_of_records = "5000"
      data_storage_location     = "us"
    }
    secrets_wo = {
      # Both are secret for this method per the vendor catalog.
      client_id     = var.salesforce_client_id
      client_secret = var.salesforce_client_secret
    }
    secrets_wo_version = 1
  }

  scans = { "data-inspection" = { enabled = true } }
}
