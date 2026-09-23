# InHost BYOK: Atlassian Jira via API key, credentials in AWS Secrets Manager
#
# For an InHost BYOK ("bring your own key") connection, the credentials never reach Relyance.
# You keep every credential field of the auth method (secret and non-secret) in one AWS Secrets
# Manager secret in your own AWS account, as a JSON object keyed by field name. Relyance stores
# only the secret's ARN (secret_ref). The InHost scanner reads the secret at scan time.
#
# Requirements:
# - The tenant has an enabled InHost deployment (Outpost on AWS). One apply creates the
#   connection, sets runtime_mode = "IN_HOST_BYOK", and then saves secret_ref.
# - The secret must be in the AWS account of your InHost deployment, and its region must be
#   in the ARN's partition.
# - The scanner's IAM role needs secretsmanager:GetSecretValue on the secret (and kms:Decrypt
#   if the secret uses a customer-managed KMS key). The Relyance InHost AWS Terraform module
#   grants it when you add a matching pattern, such as
#   "arn:aws:secretsmanager:us-east-1:123456789012:secret:relyance/inhost/*", to its
#   byok_secret_arn_patterns variable.
# - Do not set auth.secrets_wo. auth.params may hold only top-level fields, such as
#   data_storage_location (is_top_level = true in the relyance_integration_vendor data source).

resource "aws_secretsmanager_secret" "jira" {
  name        = "relyance/inhost/jira"
  description = "Jira API key credentials for the Relyance InHost scanner"
}

resource "aws_secretsmanager_secret_version" "jira" {
  secret_id = aws_secretsmanager_secret.jira.id

  # Every credential field of the "api-key" method, keyed by field name. These are
  # placeholders: set the real values outside version control (for example, with the AWS
  # console or CLI) and keep them out of Terraform state.
  secret_string = jsonencode({
    ORG_ID                = "REPLACE_ME"
    API_KEY               = "REPLACE_ME"
    data_storage_location = "us"
  })

  lifecycle {
    # The value is managed outside Terraform: you rotate it, and the scanner can write
    # refreshed credentials back to it.
    ignore_changes = [secret_string]
  }
}

resource "relyance_integration_connection" "jira_byok" {
  vendor       = "atlassian_jira"
  name         = "Jira (InHost BYOK)"
  runtime_mode = "IN_HOST_BYOK"

  auth = {
    method = "api-key"
    params = {
      # Top-level fields are still sent to Relyance.
      data_storage_location = "us"
    }
  }

  # Only the ARN is sent to Relyance. Remove this line to clear the reference.
  secret_ref = aws_secretsmanager_secret.jira.arn

  scans = {
    "vendor-discovery" = { enabled = true }
  }

  depends_on = [aws_secretsmanager_secret_version.jira]
}
