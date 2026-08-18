provider "relyance" {
  # endpoint defaults to the production Relyance API; set it only to target a
  # non-production environment. Env: RELYANCE_ENDPOINT
  client_id = var.relyance_client_id

  # One of client_secret / jwk_json (private_key_jwt):
  client_secret = var.relyance_client_secret
}
