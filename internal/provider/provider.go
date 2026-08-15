// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package provider implements the `relyance` Terraform provider — an
// umbrella provider for Relyance's tenant-facing management APIs. v1 covers
// integration config management (integrations-api); future domains add their
// own resource packages here.
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/relyance/terraform-provider-relyance/internal/provider/businessnodes"
	"github.com/relyance/terraform-provider-relyance/internal/provider/catalogvendor"
	"github.com/relyance/terraform-provider-relyance/internal/provider/connection"
	"github.com/relyance/terraform-provider-relyance/internal/provider/connectiondata"
)

type relyanceProvider struct{ version string }

// New returns the provider factory. version is injected from the release
// build (ldflags) — "dev" for local builds.
func New(version string) func() tfprovider.Provider {
	return func() tfprovider.Provider { return &relyanceProvider{version: version} }
}

func (p *relyanceProvider) Metadata(_ context.Context, _ tfprovider.MetadataRequest, resp *tfprovider.MetadataResponse) {
	resp.TypeName = "relyance"
	resp.Version = p.version
}

func (p *relyanceProvider) Schema(_ context.Context, _ tfprovider.SchemaRequest, resp *tfprovider.SchemaResponse) {
	resp.Schema = providerSchema()
}

func (p *relyanceProvider) Configure(ctx context.Context, req tfprovider.ConfigureRequest, resp *tfprovider.ConfigureResponse) {
	configureProvider(ctx, req, resp)
}

func (p *relyanceProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		connection.NewResource,
	}
}

func (p *relyanceProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		businessnodes.NewDataSource,
		catalogvendor.NewDataSource,
		connectiondata.NewDataSource,
	}
}
