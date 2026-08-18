// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package connectiondata exposes an existing integration connection as a
// read-only data source — for referencing connections Terraform observes but
// does not own (e.g. OAuth-vendor connections authorized in the browser, to
// gate on their auth_status).
package connectiondata

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/relyance/terraform-provider-relyance/internal/client"
	"github.com/relyance/terraform-provider-relyance/internal/common"
)

var (
	_ datasource.DataSource              = &connDataSource{}
	_ datasource.DataSourceWithConfigure = &connDataSource{}
)

// NewDataSource returns the relyance_integration_connection data source.
func NewDataSource() datasource.DataSource { return &connDataSource{} }

type connDataSource struct {
	c *client.Client
}

type model struct {
	Vendor          types.String `tfsdk:"vendor"`
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	IntegrationType types.String `tfsdk:"integration_type"`
	AuthStatus      types.String `tfsdk:"auth_status"`
	AuthType        types.String `tfsdk:"auth_type"`
}

func (d *connDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration_connection"
}

func (d *connDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An existing integration connection (read-only). Useful for referencing " +
			"connections managed outside Terraform — e.g. gating on a browser-authorized " +
			"OAuth connection's auth_status.",
		Attributes: map[string]schema.Attribute{
			"vendor":           schema.StringAttribute{Required: true, Description: "Vendor identifier, e.g. aws_s3."},
			"id":               schema.StringAttribute{Required: true, Description: "Connection id."},
			"name":             schema.StringAttribute{Computed: true, Description: "Display name."},
			"integration_type": schema.StringAttribute{Computed: true, Description: "Integration type."},
			"auth_status":      schema.StringAttribute{Computed: true, Description: "Live credential status (e.g. AUTH_STATUS_CONNECTED)."},
			"auth_type":        schema.StringAttribute{Computed: true, Description: "Configured authentication method."},
		},
	}
}

func (d *connDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	bundle, ok := req.ProviderData.(interface{ GetClient() *client.Client })
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	d.c = bundle.GetClient()
}

func (d *connDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.c == nil {
		resp.Diagnostics.AddError("Provider not configured", "connection data source used before Configure")
		return
	}
	var cfg model
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	detail, err := d.c.GetConnection(ctx, cfg.Vendor.ValueString(), cfg.ID.ValueString())
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			resp.Diagnostics.AddError("Connection not found",
				fmt.Sprintf("no connection %s/%s in this tenant", cfg.Vendor.ValueString(), cfg.ID.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Reading connection", err.Error())
		return
	}

	out := model{Vendor: cfg.Vendor, ID: cfg.ID}
	out.Name = strOrNull(detail, "connection_name")
	out.IntegrationType = strOrNull(detail, "integrationType")
	if a := detail.Auth(); a != nil {
		out.AuthStatus = anyStr(a["status"])
		out.AuthType = anyStr(a["type"])
	} else {
		out.AuthStatus = types.StringNull()
		out.AuthType = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

func strOrNull(d *client.ConnectionDetail, key string) types.String {
	if v, ok := d.StringField(key); ok {
		return types.StringValue(v)
	}
	return types.StringNull()
}

func anyStr(v any) types.String {
	if s, ok := v.(string); ok {
		return types.StringValue(s)
	}
	return types.StringNull()
}
