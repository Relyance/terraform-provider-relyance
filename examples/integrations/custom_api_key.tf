# Custom / arbitrary third-party API via API key
#
# "relyance_customapiimport" is the generic connector for pulling from a third-party API
# that isn't a first-class vendor. The "api-key" method sends a single bearer/API key
# (secret) plus a json_config describing the endpoints/response mapping. It also supports
# oauth-client-credentials, username-password, key-pair, etc. — see the vendor data source.
resource "relyance_integration_connection" "custom_api" {
  vendor = "relyance_customapiimport"
  name   = "Acme internal API (API key)"

  auth = {
    method = "api-key"
    params = {
      auth_method         = "header" # how the key is sent, e.g. header/bearer
      api_response_format = "json"
      target_vendor       = "acme"
      internal_service    = "false"
      # json_config is a JSON blob describing endpoints + field mapping; see the vendor data source.
      json_config           = jsonencode({ base_url = "https://api.acme.example", endpoints = [] })
      data_storage_location = "us"
    }
    secrets_wo         = { api_key = var.acme_api_key }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection" = { enabled = true }
  }
}

variable "acme_api_key" {
  type      = string
  sensitive = true
}
