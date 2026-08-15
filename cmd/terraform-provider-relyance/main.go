// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Command terraform-provider-relyance serves the Relyance Terraform provider
// plugin to Terraform over the plugin-framework protocol.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/relyance/terraform-provider-relyance/internal/provider"
)

// version is set by goreleaser via ldflags at release time.
var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "run the provider in debug mode for debugger attachment")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/relyance/relyance",
		Debug:   *debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
