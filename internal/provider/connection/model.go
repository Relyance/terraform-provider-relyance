// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package connection implements the relyance_integration_connection resource and
// its data-source form: schema, plan/state model conversions, and the fan-out of a
// single Terraform apply to the connection/auth/kinds/selection API calls.
package connection

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

// rfc3339ToEpoch parses a practitioner-facing RFC3339 timestamp into the
// API's epoch-seconds representation.
func rfc3339ToEpoch(attr path.Path, v string, diags *diag.Diagnostics) *float64 {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		diags.AddAttributeError(attr, "Invalid timestamp",
			fmt.Sprintf("expected RFC3339 (e.g. 2026-08-01T00:00:00Z), got %q", v))
		return nil
	}
	e := float64(t.Unix())
	return &e
}

// epochToRFC3339 renders the API's epoch seconds as RFC3339 UTC.
func epochToRFC3339(v float64) string {
	return time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
}

// putLocationField adds a set, non-empty Location field to the wire dict. The
// server drops empty values, so mirroring that keeps config and read-back aligned.
func putLocationField(m map[string]string, key string, v types.String) {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		m[key] = v.ValueString()
	}
}

// nullIfEmpty maps an absent/empty server Location string to a null attribute.
func nullIfEmpty(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// resourceModel is the Terraform state/plan shape for
// relyance_integration_connection (Phase 1: scalars only; auth/kinds arrive
// as additive Optional attributes in later phases).
type resourceModel struct {
	ID                  types.String              `tfsdk:"id"`
	Vendor              types.String              `tfsdk:"vendor"`
	Name                types.String              `tfsdk:"name"`
	IntegrationType     types.String              `tfsdk:"integration_type"`
	RefreshFrequency    types.Int64               `tfsdk:"refresh_frequency_seconds"`
	ScanFrom            types.String              `tfsdk:"scan_from"`
	DataStorageLocation *dataStorageLocationModel `tfsdk:"data_storage_location"`
	BusinessNodeIDs     types.Set                 `tfsdk:"business_node_ids"`
	CredentialsExpireAt types.String              `tfsdk:"credentials_expire_at"`
	SupportSecretAccess types.Bool                `tfsdk:"support_secret_access"`
	Auth                *authModel                `tfsdk:"auth"`
	Scans               types.Map                 `tfsdk:"scans"`
	ConnectOnApply      types.Bool                `tfsdk:"connect_on_apply"`
}

// dataStorageLocationModel is the Relyance Location shape (region/country/state,
// all optional) the server persists under data_storage_location and stringifies
// as "region|country|state" — mirrors relyance_sdk's data_storage_location field.
type dataStorageLocationModel struct {
	Region  types.String `tfsdk:"region"`
	Country types.String `tfsdk:"country"`
	State   types.String `tfsdk:"state"`
}

// scanModel is one entry of the scans map (keyed by scan/kind slug).
type scanModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
	Fields  types.Map  `tfsdk:"fields"`
}

func scanObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"enabled": types.BoolType,
		"fields":  types.MapType{ElemType: types.StringType},
	}}
}

// authModel is the nested auth block. SecretsWO is write-only: its value is
// only ever present in CONFIG (never plan or state), so any code needing
// secret values must read req.Config, and state writes must leave it null.
type authModel struct {
	Method                 types.String `tfsdk:"method"`
	Params                 types.Map    `tfsdk:"params"`
	SecretsWO              types.Map    `tfsdk:"secrets_wo"`
	SecretsWOVersion       types.Int64  `tfsdk:"secrets_wo_version"`
	Status                 types.String `tfsdk:"status"`
	CredentialsFingerprint types.String `tfsdk:"credentials_fingerprint"`
	UpdatedAt              types.String `tfsdk:"updated_at"`
}

// mergeAuthCreds unions params and secrets into the single customCreds map
// the API takes. A key present in both is a config error.
func mergeAuthCreds(ctx context.Context, params, secrets types.Map) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := map[string]any{}
	read := func(m types.Map) map[string]string {
		if m.IsNull() || m.IsUnknown() {
			return nil
		}
		var vals map[string]string
		diags.Append(m.ElementsAs(ctx, &vals, false)...)
		return vals
	}
	p := read(params)
	sec := read(secrets)
	if diags.HasError() {
		return nil, diags
	}
	for k, v := range p {
		out[k] = v
	}
	for k, v := range sec {
		if _, dup := out[k]; dup {
			diags.AddAttributeError(path.Root("auth").AtName("secrets_wo"), "Duplicate auth field",
				fmt.Sprintf("field %q appears in both auth.params and auth.secrets_wo", k))
			return nil, diags
		}
		out[k] = v
	}
	return out, diags
}

// authChanged reports whether the planned auth block requires a PUT .../auth:
// method, params, or the rotation version differ from state. Secret VALUES
// never participate (write-only values cannot be diffed — that is what
// secrets_wo_version is for).
func authChanged(plan, state *authModel) bool {
	if plan == nil {
		return false
	}
	if state == nil {
		return true
	}
	return !plan.Method.Equal(state.Method) ||
		!plan.Params.Equal(state.Params) ||
		!plan.SecretsWOVersion.Equal(state.SecretsWOVersion)
}

// refreshAuthComputed copies server-owned auth fields into the model; params
// and secrets are never touched (masked reads are not state).
func (m *resourceModel) refreshAuthComputed(d *client.ConnectionDetail) {
	if m.Auth == nil {
		return
	}
	auth := d.Auth()
	if auth == nil {
		m.Auth.Status = types.StringNull()
		m.Auth.CredentialsFingerprint = types.StringNull()
		m.Auth.UpdatedAt = types.StringNull()
		return
	}
	if v, ok := auth["status"].(string); ok {
		m.Auth.Status = types.StringValue(v)
	} else {
		m.Auth.Status = types.StringNull()
	}
	if v, ok := auth["credentials_fingerprint"].(string); ok {
		m.Auth.CredentialsFingerprint = types.StringValue(v)
	} else {
		m.Auth.CredentialsFingerprint = types.StringNull()
	}
	if v, ok := auth["updated_at"].(float64); ok {
		m.Auth.UpdatedAt = types.StringValue(epochToRFC3339(v))
	} else {
		m.Auth.UpdatedAt = types.StringNull()
	}
}

// toScalarUpdate builds the PATCH body from the planned model. Only fields
// the practitioner set (non-null) are sent; the server leaves omitted fields
// untouched.
func (m *resourceModel) toScalarUpdate(ctx context.Context) (client.ScalarUpdateRequest, diag.Diagnostics) {
	var req client.ScalarUpdateRequest
	var diags diag.Diagnostics
	if !m.Name.IsNull() && !m.Name.IsUnknown() {
		v := m.Name.ValueString()
		req.ConnectionName = &v
	}
	if !m.RefreshFrequency.IsNull() && !m.RefreshFrequency.IsUnknown() {
		v := m.RefreshFrequency.ValueInt64()
		req.RefreshFrequency = &v
	}
	if !m.ScanFrom.IsNull() && !m.ScanFrom.IsUnknown() {
		req.StartScanFrom = rfc3339ToEpoch(path.Root("scan_from"), m.ScanFrom.ValueString(), &diags)
	}
	if m.DataStorageLocation != nil {
		dsl := map[string]string{}
		putLocationField(dsl, "region", m.DataStorageLocation.Region)
		putLocationField(dsl, "country", m.DataStorageLocation.Country)
		putLocationField(dsl, "state", m.DataStorageLocation.State)
		// Don't send an empty object — the server would clear the stored value.
		if len(dsl) > 0 {
			req.DataStorageLocation = dsl
		}
	}
	if !m.BusinessNodeIDs.IsNull() && !m.BusinessNodeIDs.IsUnknown() {
		var ids []string
		diags.Append(m.BusinessNodeIDs.ElementsAs(ctx, &ids, false)...)
		req.BusinessNodeIDs = ids
	}
	if !m.CredentialsExpireAt.IsNull() && !m.CredentialsExpireAt.IsUnknown() {
		req.CredentialsExpireAt = rfc3339ToEpoch(path.Root("credentials_expire_at"), m.CredentialsExpireAt.ValueString(), &diags)
	}
	if !m.SupportSecretAccess.IsNull() && !m.SupportSecretAccess.IsUnknown() {
		v := m.SupportSecretAccess.ValueBool()
		req.RelyanceSecretAccess = &v
	}
	return req, diags
}

// refreshFromAPI overlays server truth onto the model for the fields the
// server owns or that the practitioner set. Optional-only attributes the
// practitioner never set stay null even when the server reports a value
// (server-side defaults are not config drift).
func (m *resourceModel) refreshFromAPI(ctx context.Context, d *client.ConnectionDetail) diag.Diagnostics {
	var diags diag.Diagnostics

	if v, ok := d.StringField("connection_name"); ok {
		m.Name = types.StringValue(v)
	}
	// integration_type is Optional+Computed: it MUST be known (value or null)
	// after apply, never left unknown.
	if v, ok := d.StringField("integrationType"); ok {
		m.IntegrationType = types.StringValue(v)
	} else {
		m.IntegrationType = types.StringNull()
	}
	if !m.RefreshFrequency.IsNull() {
		if v, ok := d.NumberField("refreshFrequency"); ok {
			m.RefreshFrequency = types.Int64Value(int64(v))
		}
	}
	if !m.ScanFrom.IsNull() {
		if v, ok := d.NumberField("startScanFrom"); ok {
			// Keep the practitioner's rendering when it denotes the same instant
			// (a +05:30 offset equals its Z-normalized form); only overwrite on
			// real drift, so a non-UTC config value doesn't look like a change.
			var pdiags diag.Diagnostics
			cur := rfc3339ToEpoch(path.Root("scan_from"), m.ScanFrom.ValueString(), &pdiags)
			if pdiags.HasError() || cur == nil || int64(*cur) != int64(v) {
				m.ScanFrom = types.StringValue(epochToRFC3339(v))
			}
		}
	}
	if m.DataStorageLocation != nil {
		// Stored under the snake_case doc key (routes.py writes data_storage_location).
		if v, ok := d.MapStringField("data_storage_location"); ok {
			m.DataStorageLocation = &dataStorageLocationModel{
				Region:  nullIfEmpty(v["region"]),
				Country: nullIfEmpty(v["country"]),
				State:   nullIfEmpty(v["state"]),
			}
		}
	}
	if !m.BusinessNodeIDs.IsNull() {
		if ids, ok := d.StringSliceField("business_node_ids"); ok {
			set, sdiags := types.SetValueFrom(ctx, types.StringType, ids)
			diags.Append(sdiags...)
			if !sdiags.HasError() {
				m.BusinessNodeIDs = set
			}
		}
	}
	if !m.SupportSecretAccess.IsNull() {
		if v, ok := d.BoolField("relyanceSecretAccess"); ok {
			m.SupportSecretAccess = types.BoolValue(v)
		}
	}
	return diags
}

// scansFromMap decodes the scans map into ordered slug->scanModel entries.
func scansFromMap(ctx context.Context, m types.Map) (map[string]scanModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	if m.IsNull() || m.IsUnknown() {
		return nil, diags
	}
	out := map[string]scanModel{}
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out, diags
}

// applyScans reconciles the desired scans against the prior state, issuing the
// minimal set of select/save calls. Returns the ordered actions for testing.
func applyScans(ctx context.Context, svc Service, vendorKey, id string, plan, state types.Map) diag.Diagnostics {
	var diags diag.Diagnostics
	desired, d := scansFromMap(ctx, plan)
	diags.Append(d...)
	prior, d := scansFromMap(ctx, state)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	for slug, want := range desired {
		had, existed := prior[slug]
		if !existed || !had.Enabled.Equal(want.Enabled) {
			if err := svc.SelectKind(ctx, vendorKey, id, slug, want.Enabled.ValueBool()); err != nil {
				diags.AddAttributeError(path.Root("scans").AtMapKey(slug), "Selecting scan", err.Error())
				return diags
			}
		}
		if want.Enabled.ValueBool() && (!existed || !had.Fields.Equal(want.Fields)) {
			params, d := fieldsToParams(ctx, want.Fields)
			diags.Append(d...)
			if diags.HasError() {
				return diags
			}
			if len(params) > 0 {
				if err := svc.SaveKind(ctx, vendorKey, id, slug, params); err != nil {
					diags.AddAttributeError(path.Root("scans").AtMapKey(slug), "Saving scan fields", err.Error())
					return diags
				}
			}
		}
	}
	// Deselect scans removed from config.
	for slug := range prior {
		if _, still := desired[slug]; !still {
			if err := svc.SelectKind(ctx, vendorKey, id, slug, false); err != nil {
				diags.AddAttributeError(path.Root("scans"), "Disabling removed scan", err.Error())
				return diags
			}
		}
	}
	return diags
}

// fieldsToParams converts a scan fields map to the API's customFields list.
func fieldsToParams(ctx context.Context, m types.Map) ([]map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	if m.IsNull() || m.IsUnknown() {
		return nil, diags
	}
	var kv map[string]string
	diags.Append(m.ElementsAs(ctx, &kv, false)...)
	out := make([]map[string]any, 0, len(kv))
	for k, v := range kv {
		out = append(out, map[string]any{"key": k, "value": v})
	}
	return out, diags
}

// refreshScans overlays server kind state onto the scans map for kinds the
// practitioner manages (present in prior state); it never invents entries.
func (m *resourceModel) refreshScans(ctx context.Context, d *client.ConnectionDetail) diag.Diagnostics {
	var diags diag.Diagnostics
	if m.Scans.IsNull() || m.Scans.IsUnknown() {
		return diags
	}
	desired, d2 := scansFromMap(ctx, m.Scans)
	diags.Append(d2...)
	if diags.HasError() {
		return diags
	}
	// Build slug -> server kind state from the connection's kinds sub-doc.
	kinds, _ := d.Connection["kinds"].(map[string]any)
	serverBySlug := map[string]map[string]any{}
	for kindEnum, v := range kinds {
		if kv, ok := v.(map[string]any); ok {
			serverBySlug[kindSlug(kindEnum)] = kv
		}
	}
	out := map[string]scanModel{}
	for slug, want := range desired {
		sv, ok := serverBySlug[slug]
		if !ok {
			out[slug] = want
			continue
		}
		enabled := want.Enabled
		if b, ok := sv["isSelected"].(bool); ok {
			enabled = types.BoolValue(b)
		}
		out[slug] = scanModel{Enabled: enabled, Fields: want.Fields}
	}
	m2, d3 := types.MapValueFrom(ctx, scanObjectType(), out)
	diags.Append(d3...)
	if !diags.HasError() {
		m.Scans = m2
	}
	return diags
}

// kindSlugs is the server's fixed kind->slug table (services/catalog/kind_naming.py),
// copied rather than derived: most slugs match a mechanical strip+kebab, but DSRS is
// the exception (data-subject-requests, not dsrs) — a derivation would silently key
// its scan state wrong and miss drift on is_selected.
var kindSlugs = map[string]string{
	"INTEGRATION_KIND_PROPERTY_INSPECTION":     "property-inspection",
	"INTEGRATION_KIND_DATA_INSPECTION":         "data-inspection",
	"INTEGRATION_KIND_DSRS":                    "data-subject-requests",
	"INTEGRATION_KIND_VENDOR_DISCOVERY":        "vendor-discovery",
	"INTEGRATION_KIND_ASSETS_DISCOVERY":        "assets-discovery",
	"INTEGRATION_KIND_CONTRACT_SCAN":           "contract-scan",
	"INTEGRATION_KIND_AI_DISCOVERY":            "ai-discovery",
	"INTEGRATION_KIND_AI_MODEL_DISCOVERY":      "ai-model-discovery",
	"INTEGRATION_KIND_DISCOVERY_CONFIGURATION": "discovery-configuration",
	"INTEGRATION_KIND_SSO_VENDOR_DISCOVERY":    "sso-vendor-discovery",
	"INTEGRATION_KIND_FIELD_MAPPING":           "field-mapping",
}

// kindSlug maps a kind enum to its customer-facing slug. Unknown enums (a server that
// adds a kind before this table does) fall back to the mechanical strip+kebab.
func kindSlug(kindEnum string) string {
	if s, ok := kindSlugs[kindEnum]; ok {
		return s
	}
	s := strings.TrimPrefix(kindEnum, "INTEGRATION_KIND_")
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, "_", "-")
}
