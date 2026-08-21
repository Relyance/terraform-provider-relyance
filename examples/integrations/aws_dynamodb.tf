# Amazon DynamoDB: classify data in DynamoDB tables via an assumed IAM role.
resource "relyance_integration_connection" "dynamodb" {
  vendor = "aws_dynamodb"
  name   = "Production DynamoDB"

  auth = {
    method = "iam-role-direct"
    params = {
      account_id = "123456789012"
      role_name  = "RelyanceDynamoDBScanRole"
      region     = "us-east-1"
      tables     = jsonencode(["customers", "orders"])
    }
    secrets_wo         = { external_id = var.dynamodb_external_id }
    secrets_wo_version = 1
  }

  scans = { "data-inspection" = { enabled = true } }
}
