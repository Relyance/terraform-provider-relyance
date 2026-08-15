# Reference a connection Terraform doesn't own (e.g. a browser-authorized OAuth
# vendor) — gate on its live status:
data "relyance_integration_connection" "sfdc" {
  vendor = "salesforce"
  id     = "0"
}

output "salesforce_connected" {
  value = data.relyance_integration_connection.sfdc.auth_status == "AUTH_STATUS_CONNECTED"
}
