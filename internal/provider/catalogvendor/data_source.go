// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package catalogvendor exposes the vendor catalog entry as a data source —
// the practitioner-facing reference for a vendor's auth forms and field
// schemas (including which fields are secret).
package catalogvendor

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
	_ datasource.DataSource              = &vendorDataSource{}
	_ datasource.DataSourceWithConfigure = &vendorDataSource{}
)

// NewDataSource returns the relyance_integration_vendor data source.
func NewDataSource() datasource.DataSource { return &vendorDataSource{} }

type vendorDataSource struct {
	c *client.Client
}

type customFieldModel struct {
	Key          types.String `tfsdk:"key"`
	Name         types.String `tfsdk:"name"`
	DefaultValue types.String `tfsdk:"default_value"`
	IsSecret     types.Bool   `tfsdk:"is_secret"`
	FieldType    types.String `tfsdk:"field_type"`
}

type authMethodModel struct {
	Method      types.String       `tfsdk:"method"`
	DisplayName types.String       `tfsdk:"display_name"`
	LegacyKey   types.String       `tfsdk:"legacy_key"`
	Fields      []customFieldModel `tfsdk:"fields"`
}

type vendorModel struct {
	Vendor      types.String      `tfsdk:"vendor"`
	Type        types.String      `tfsdk:"type"`
	AuthMethods []authMethodModel `tfsdk:"auth_methods"`
}

func (d *vendorDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration_vendor"
}

func (d *vendorDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A vendor available to connect (filtered to what this tenant is licensed for): " +
			"its authentication methods and their field schemas, including which fields are secret.",
		Attributes: map[string]schema.Attribute{
			"vendor": schema.StringAttribute{
				Required:    true,
				Description: "Vendor identifier, e.g. aws_s3.",
			},
			"type": schema.StringAttribute{
				Computed:    true,
				Description: "Integration type.",
			},
			"auth_methods": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Authentication methods available for this vendor.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"method":       schema.StringAttribute{Computed: true, Description: "Method identifier (e.g. iam-role-direct, api-key, oauth) — the value for the connection's auth.method."},
						"display_name": schema.StringAttribute{Computed: true, Description: "Human-readable method name."},
						"legacy_key":   schema.StringAttribute{Computed: true, Description: "Internal legacy identifier, accepted for compatibility."},
						"fields": schema.ListNestedAttribute{
							Computed:    true,
							Description: "Field schema for this method.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"key":           schema.StringAttribute{Computed: true, Description: "Field key used in auth.params / auth.secrets_wo."},
									"name":          schema.StringAttribute{Computed: true, Description: "Display name."},
									"default_value": schema.StringAttribute{Computed: true, Description: "Server-side default value."},
									"is_secret":     schema.BoolAttribute{Computed: true, Description: "True → this field belongs in auth.secrets (write-only), never auth.params."},
									"field_type":    schema.StringAttribute{Computed: true, Description: "Field type hint (e.g. FIELD_TYPE_LOCATION_SELECT)."},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *vendorDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *vendorDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.c == nil {
		resp.Diagnostics.AddError("Provider not configured", "catalog vendor data source used before Configure")
		return
	}
	var cfg vendorModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vendor, err := d.c.GetVendor(ctx, cfg.Vendor.ValueString())
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			resp.Diagnostics.AddError("Vendor not found",
				fmt.Sprintf("vendor %q is not in this tenant's catalog (check licensing/feature flags)", cfg.Vendor.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Reading catalog vendor", err.Error())
		return
	}

	out := vendorModel{
		Vendor: types.StringValue(vendor.VendorKey),
		Type:   types.StringValue(vendor.Type),
	}
	for _, ac := range vendor.AuthConfigs {
		acm := authMethodModel{
			Method:      types.StringValue(ac.Slug),
			DisplayName: types.StringValue(ac.DisplayName),
			LegacyKey:   types.StringValue(ac.Key),
		}
		for _, f := range ac.CustomFields {
			acm.Fields = append(acm.Fields, customFieldModel{
				Key:          types.StringValue(f.Key),
				Name:         types.StringValue(f.Name),
				DefaultValue: types.StringValue(f.DefaultValue),
				IsSecret:     types.BoolValue(f.IsThisSecret),
				FieldType:    types.StringValue(f.FieldType),
			})
		}
		out.AuthMethods = append(out.AuthMethods, acm)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}
