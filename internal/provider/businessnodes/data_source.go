// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package businessnodes exposes the tenant's business nodes as a lookup data
// source: name→id and id→name maps, so connection resources can reference
// nodes by name instead of opaque record ids.
package businessnodes

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

var (
	_ datasource.DataSource              = &nodesDataSource{}
	_ datasource.DataSourceWithConfigure = &nodesDataSource{}
)

// NewDataSource returns the relyance_business_nodes data source.
func NewDataSource() datasource.DataSource { return &nodesDataSource{} }

type nodesDataSource struct {
	c *client.Client
}

type nodeModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Type types.String `tfsdk:"type"`
}

type nodesModel struct {
	Nodes  []nodeModel `tfsdk:"nodes"`
	ByName types.Map   `tfsdk:"by_name"`
	ByID   types.Map   `tfsdk:"by_id"`
}

func (d *nodesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_business_nodes"
}

func (d *nodesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The tenant's business nodes, as lookup maps. Use by_name to reference nodes " +
			"in connection arguments without knowing record ids: " +
			"business_node_ids = [data.relyance_business_nodes.all.by_name[\"Engineering\"]].",
		Attributes: map[string]schema.Attribute{
			"nodes": schema.ListNestedAttribute{
				Computed:    true,
				Description: "All business nodes.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":   schema.StringAttribute{Computed: true, Description: "Record id."},
						"name": schema.StringAttribute{Computed: true, Description: "Display name."},
						"type": schema.StringAttribute{Computed: true, Description: "Node type (business entity or product)."},
					},
				},
			},
			"by_name": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Map of node name → record id.",
			},
			"by_id": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Map of record id → node name.",
			},
		},
	}
}

func (d *nodesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *nodesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.c == nil {
		resp.Diagnostics.AddError("Provider not configured", "business nodes data source used before Configure")
		return
	}
	nodes, err := d.c.ListBusinessNodes(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Listing business nodes", err.Error())
		return
	}

	out := nodesModel{}
	byName := make(map[string]string, len(nodes))
	byID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, nodeModel{
			ID:   types.StringValue(n.ID),
			Name: types.StringValue(n.Name),
			Type: types.StringValue(n.Type),
		})
		if prev, dup := byName[n.Name]; dup {
			resp.Diagnostics.AddWarning(
				"Duplicate business node name",
				fmt.Sprintf("business node name %q maps to multiple ids (%s, %s); by_name keeps the first — reference the id directly to disambiguate", n.Name, prev, n.ID),
			)
		} else {
			byName[n.Name] = n.ID
		}
		byID[n.ID] = n.Name
	}

	var diags = &resp.Diagnostics
	nameMap, d1 := types.MapValueFrom(ctx, types.StringType, byName)
	diags.Append(d1...)
	idMap, d2 := types.MapValueFrom(ctx, types.StringType, byID)
	diags.Append(d2...)
	if diags.HasError() {
		return
	}
	out.ByName = nameMap
	out.ByID = idMap
	diags.Append(resp.State.Set(ctx, &out)...)
}
