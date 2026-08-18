data "relyance_business_nodes" "all" {}

# Reference nodes by name in connection arguments:
# business_node_ids = [data.relyance_business_nodes.all.by_name["Engineering"]]

output "node_names" {
  value = keys(data.relyance_business_nodes.all.by_name)
}
