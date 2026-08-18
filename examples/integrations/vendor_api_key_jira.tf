# Third-party SaaS vendor via API key — Atlassian Jira
#
# Many SaaS vendors authenticate with an API key/token. For Jira's "api-key" method,
# both ORG_ID and API_KEY are secret (write-only); data_storage_location is a plain param.
# (Jira also offers "oauth" and "api-token" methods — see the vendor data source.)
resource "relyance_integration_connection" "jira" {
  vendor = "atlassian_jira"
  name   = "Jira (API key)"

  auth = {
    method = "api-key"
    params = {
      data_storage_location = "us"
    }
    secrets_wo = {
      ORG_ID  = var.jira_org_id
      API_KEY = var.jira_api_key
    }
    secrets_wo_version = 1
  }

  scans = {
    "vendor-discovery" = { enabled = true }
  }
}

variable "jira_org_id" {
  type      = string
  sensitive = true
}
variable "jira_api_key" {
  type      = string
  sensitive = true
}
