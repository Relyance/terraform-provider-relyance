data "relyance_integration_vendor" "s3" {
  vendor = "aws_s3"
}

output "s3_auth_methods" {
  value = data.relyance_integration_vendor.s3.auth_methods[*].method
}
