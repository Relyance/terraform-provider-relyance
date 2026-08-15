// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// accConnConfig renders a throwaway aws_s3 connection with the given display name.
// External id is a write-only secret; account id is a placeholder (the connection is
// created and destroyed within the test and never actually authorizes to AWS).
func accConnConfig(name string) string {
	return fmt.Sprintf(`
resource "relyance_integration_connection" "test" {
  vendor = "aws_s3"
  name   = %q

  auth = {
    method = "iam-role-direct"
    params = {
      account_id = "123456789012"
      role_name  = "tf-acc-role"
      region     = "us-east-2"
    }
    secrets_wo         = { external_id = "tf-acc-external-id" }
    secrets_wo_version = 1
  }

  scans = {
    "data-inspection" = { enabled = true }
  }
}`, name)
}

// TestAccConnectionResource exercises the connection resource end-to-end against a live
// tenant: create (auth-by-slug, scan-by-slug, write-only secret) -> rename -> import.
// It creates and destroys a throwaway connection; point RELYANCE_* at a non-prod tenant.
func TestAccConnectionResource(t *testing.T) {
	const res = "relyance_integration_connection.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: accConnConfig("tf-acc-DELETE-ME"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(res, "id"),
					resource.TestCheckResourceAttr(res, "vendor", "aws_s3"),
					resource.TestCheckResourceAttr(res, "name", "tf-acc-DELETE-ME"),
					resource.TestCheckResourceAttr(res, "auth.method", "iam-role-direct"),
					// server writes a fingerprint once the credential is saved
					resource.TestCheckResourceAttrSet(res, "auth.credentials_fingerprint"),
					// write-only secret is never persisted to state
					resource.TestCheckNoResourceAttr(res, "auth.secrets_wo.external_id"),
					resource.TestCheckResourceAttr(res, "scans.data-inspection.enabled", "true"),
				),
			},
			{ // rename (PATCH)
				Config: accConnConfig("tf-acc-renamed-DELETE-ME"),
				Check:  resource.TestCheckResourceAttr(res, "name", "tf-acc-renamed-DELETE-ME"),
			},
			{ // import — recovers identity + scalars (auth/scans come from config, not import).
				// Import id is the composite "<vendor>/<connection_id>", not the bare id in state.
				ResourceName:      res,
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources[res]
					if !ok {
						return "", fmt.Errorf("resource %s not found in state", res)
					}
					return "aws_s3/" + rs.Primary.ID, nil
				},
			},
		},
	})
}
