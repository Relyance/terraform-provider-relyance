# Snowflake via key-pair authentication (recommended for automation: asymmetric,
# rotatable, no shared password).
resource "relyance_integration_connection" "snowflake" {
  vendor = "snowflake"
  name   = "Snowflake production"

  auth = {
    method = "key-pair"
    params = {
      account       = "myorg-prod"
      user          = "RELYANCE_SVC"
      role          = "RELYANCE_SCANNER"
      use_warehouse = "RELYANCE_WH"
      config_options = jsonencode({
        allow_list = []
        deny_list  = ["SNOWFLAKE_SAMPLE_DATA.*"]
      })
      data_storage_location = "us"
    }
    secrets_wo = {
      # PEM-encoded private key for the Snowflake user.
      private_key = var.snowflake_private_key
      # The catalog marks skip_views as a secret field, so it belongs here.
      skip_views = "false"
    }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection"  = { enabled = true }
    "assets-discovery" = { enabled = true }
  }
}
