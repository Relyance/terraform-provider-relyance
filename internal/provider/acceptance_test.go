// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories wires the in-process provider for acceptance tests.
// Acceptance tests only run when TF_ACC is set (resource.Test skips otherwise).
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"relyance": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck fails fast when the environment required to reach a live tenant is
// missing, so an acceptance run surfaces a clear message instead of an auth error deep
// in a step. Point these at a NON-PRODUCTION tenant: the resource tests create and
// destroy real connections.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, k := range []string{"RELYANCE_ENDPOINT", "RELYANCE_CLIENT_ID"} {
		if os.Getenv(k) == "" {
			t.Fatalf("%s must be set for TF_ACC acceptance tests", k)
		}
	}
	if os.Getenv("RELYANCE_CLIENT_SECRET") == "" && os.Getenv("RELYANCE_JWK_JSON") == "" {
		t.Fatal("RELYANCE_CLIENT_SECRET or RELYANCE_JWK_JSON must be set for TF_ACC acceptance tests")
	}
}
