// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccVendorDataSource reads the aws_s3 vendor catalog and asserts the customer-facing
// auth-method slugs are surfaced (proves auth + edge audience-swap + catalog read).
func TestAccVendorDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "relyance_integration_vendor" "s3" { vendor = "aws_s3" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.relyance_integration_vendor.s3", "vendor", "aws_s3"),
					resource.TestCheckResourceAttrSet("data.relyance_integration_vendor.s3", "auth_methods.#"),
					// iam-role-direct is aws_s3's direct-connection auth method slug.
					resource.TestCheckTypeSetElemNestedAttrs(
						"data.relyance_integration_vendor.s3", "auth_methods.*",
						map[string]string{"method": "iam-role-direct"},
					),
				),
			},
		},
	})
}

// TestAccBusinessNodesDataSource reads the tenant's business nodes and asserts the
// by_name lookup map is populated.
func TestAccBusinessNodesDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "relyance_business_nodes" "all" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.relyance_business_nodes.all", "by_name.%"),
				),
			},
		},
	})
}
