// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connection

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/relyance/terraform-provider-relyance/internal/client"
	"github.com/relyance/terraform-provider-relyance/internal/common"
)

var (
	_ resource.Resource                = &connectionResource{}
	_ resource.ResourceWithConfigure   = &connectionResource{}
	_ resource.ResourceWithImportState = &connectionResource{}
	_ resource.ResourceWithModifyPlan  = &connectionResource{}
)

// NewResource returns the relyance_integration_connection resource.
func NewResource() resource.Resource { return &connectionResource{} }

type connectionResource struct {
	svc            Service
	validateOnPlan bool
}

func (r *connectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration_connection"
}

func (r *connectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an integration connection for a vendor. The vendor's field schema " +
			"(auth methods, fields) comes from the relyance_integration_vendor data source.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Server-allocated connection id, unique within the vendor.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vendor": schema.StringAttribute{
				Required:    true,
				Description: "The vendor to connect, e.g. aws_s3 — see the relyance_integration_vendor data source. Changing it forces a new connection.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name of the connection.",
			},
			"integration_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "IntegrationType enum name (e.g. INTEGRATION_TYPE_VENDOR). Server default applies when omitted. Changing it forces a new connection.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"refresh_frequency_seconds": schema.Int64Attribute{
				Optional:    true,
				Description: "How often to rescan, in seconds.",
			},
			"scan_from": schema.StringAttribute{
				Optional:    true,
				Description: "RFC3339 timestamp; scans consider data from this point forward (e.g. 2026-08-01T00:00:00Z).",
			},
			"data_storage_location": schema.SingleNestedAttribute{
				Optional: true,
				Description: "Where the vendor stores data (Relyance Location shape). Set the parts you know; " +
					"unset parts are omitted. Values are the canonical Location strings used across ROPA/DSR.",
				Attributes: map[string]schema.Attribute{
					"region":  schema.StringAttribute{Optional: true, Description: `Region, e.g. "European Union".`},
					"country": schema.StringAttribute{Optional: true, Description: `Country, e.g. "United States".`},
					"state":   schema.StringAttribute{Optional: true, Description: "State or province."},
				},
			},
			"business_node_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Business node ids this connection is associated with.",
			},
			"credentials_expire_at": schema.StringAttribute{
				Optional:    true,
				Description: "RFC3339 timestamp when the stored credentials expire.",
			},
			"support_secret_access": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow Relyance support to access stored secrets for troubleshooting.",
			},
			"auth": schema.SingleNestedAttribute{
				Optional: true,
				Description: "Credential configuration. Omit for connections whose credentials are " +
					"managed outside Terraform (e.g. OAuth vendors authorized in the browser).",
				Attributes: map[string]schema.Attribute{
					"method": schema.StringAttribute{
						Required: true,
						Description: "Authentication method for this vendor (e.g. iam-role-direct, api-key) — " +
							"see the relyance_integration_vendor data source for available methods.",
					},
					"params": schema.MapAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Description: "Non-secret auth fields (stored in state, fully diffable).",
					},
					"secrets_wo": schema.MapAttribute{
						ElementType: types.StringType,
						Optional:    true,
						WriteOnly:   true,
						Sensitive:   true,
						Description: "Secret auth fields. Write-only: never stored in state or plan files. " +
							"Because write-only values cannot be diffed, changing a secret value alone " +
							"produces NO plan change — bump secrets_wo_version to re-send rotated secrets.",
					},
					"secrets_wo_version": schema.Int64Attribute{
						Optional: true,
						Description: "Rotation trigger for secrets_wo: bump this integer whenever secret " +
							"values change so Terraform re-sends them.",
					},
					"status": schema.StringAttribute{
						Computed:    true,
						Description: "Live credential status (e.g. AUTH_STATUS_CONNECTED). Volatile by design.",
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					"credentials_fingerprint": schema.StringAttribute{
						Computed:    true,
						Description: "Server-side hash of the stored secrets; changes when credentials change.",
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					"updated_at": schema.StringAttribute{
						Computed:    true,
						Description: "RFC3339 time of the last credential write.",
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
				},
			},
			"scans": schema.MapNestedAttribute{
				Optional: true,
				Description: "Scan capabilities to enable on this connection, keyed by scan slug " +
					"(e.g. data-inspection, assets-discovery — see the relyance_integration_vendor data source).",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{
							Required:    true,
							Description: "Whether this scan is enabled.",
						},
						"fields": schema.MapAttribute{
							ElementType: types.StringType,
							Optional:    true,
							Description: "Scan-specific configuration fields.",
						},
					},
				},
			},
			"connect_on_apply": schema.BoolAttribute{
				Optional: true,
				Description: "When true, trigger a live credential test after each apply that changes " +
					"the connection. The result is asynchronous; read auth.status on a later refresh.",
			},
		},
	}
}

func (r *connectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	bundle, ok := req.ProviderData.(interface {
		GetClient() *client.Client
		GetValidateOnPlan() bool
	})
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.svc = NewService(bundle.GetClient())
	r.validateOnPlan = bundle.GetValidateOnPlan()
}

func (r *connectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.svc == nil {
		resp.Diagnostics.AddError("Provider not configured", "connection resource used before Configure")
		return
	}
	var plan resourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.CreateConnectionRequest{ConnectionName: plan.Name.ValueString()}
	if !plan.IntegrationType.IsNull() && !plan.IntegrationType.IsUnknown() {
		v := plan.IntegrationType.ValueString()
		createReq.IntegrationType = &v
	}
	id, err := r.svc.Create(ctx, plan.Vendor.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Creating connection", err.Error())
		return
	}
	plan.ID = types.StringValue(id)

	// Persist the ID now the row exists: a later step failing must leave a tracked
	// (tainted) resource, not an orphan the next apply duplicates. Overwritten by
	// the read-back state on success.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Apply any non-name scalars the practitioner set (create only takes the
	// name; the rest are PATCH semantics).
	scalars, diags := plan.toScalarUpdate(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	scalars.ConnectionName = nil // already set by create
	if scalarsNonEmpty(scalars) {
		if err := r.svc.UpdateScalars(ctx, plan.Vendor.ValueString(), id, scalars); err != nil {
			resp.Diagnostics.AddError("Applying connection settings after create", err.Error())
			return
		}
	}

	if plan.Auth != nil {
		if !r.saveAuth(ctx, req.Config, &plan, &resp.Diagnostics) {
			return
		}
	}

	resp.Diagnostics.Append(applyScans(ctx, r.svc, plan.Vendor.ValueString(), id, plan.Scans, types.MapNull(scanObjectType()))...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.maybeConnect(ctx, &plan, &resp.Diagnostics) {
		return
	}

	if ok, errText := r.readBack(ctx, &plan); !ok {
		resp.Diagnostics.AddError("Reading connection back after write", errText)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *connectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.svc == nil {
		resp.Diagnostics.AddError("Provider not configured", "connection resource used before Configure")
		return
	}
	var state resourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	detail, err := r.svc.Get(ctx, state.Vendor.ValueString(), state.ID.ValueString())
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading connection", err.Error())
		return
	}
	resp.Diagnostics.Append(state.refreshFromAPI(ctx, detail)...)
	resp.Diagnostics.Append(state.refreshScans(ctx, detail)...)
	if state.Auth != nil {
		state.refreshAuthComputed(detail)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *connectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.svc == nil {
		resp.Diagnostics.AddError("Provider not configured", "connection resource used before Configure")
		return
	}
	var plan, state resourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID

	scalars, diags := plan.toScalarUpdate(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// credentials_expire_at is re-sent by toScalarUpdate, but the server rejects a
	// past timestamp (and conflicts if the vendor set it). Only send it when the
	// practitioner actually changed it, so an unrelated update after the date has
	// passed (e.g. a rename) still applies.
	if plan.CredentialsExpireAt.Equal(state.CredentialsExpireAt) {
		scalars.CredentialsExpireAt = nil
	}
	if scalarsNonEmpty(scalars) {
		if err := r.svc.UpdateScalars(ctx, plan.Vendor.ValueString(), plan.ID.ValueString(), scalars); err != nil {
			resp.Diagnostics.AddError("Updating connection", err.Error())
			return
		}
	}

	if authChanged(plan.Auth, state.Auth) {
		if !r.saveAuth(ctx, req.Config, &plan, &resp.Diagnostics) {
			return
		}
	}

	resp.Diagnostics.Append(applyScans(ctx, r.svc, plan.Vendor.ValueString(), plan.ID.ValueString(), plan.Scans, state.Scans)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.maybeConnect(ctx, &plan, &resp.Diagnostics) {
		return
	}

	if ok, errText := r.readBack(ctx, &plan); !ok {
		resp.Diagnostics.AddError("Reading connection back after write", errText)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// maybeConnect fires the async live-credential test when connect_on_apply is
// true, recording the (non-final) monitor status into auth.status.
func (r *connectionResource) maybeConnect(ctx context.Context, plan *resourceModel, diags *diag.Diagnostics) bool {
	if !plan.ConnectOnApply.ValueBool() {
		return true
	}
	status, err := r.svc.Connect(ctx, plan.Vendor.ValueString(), plan.ID.ValueString())
	if err != nil {
		diags.AddError("Triggering connection test", err.Error())
		return false
	}
	if plan.Auth != nil && status != nil {
		plan.Auth.Status = types.StringValue(status.Status)
	}
	return true
}

func (r *connectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.svc == nil {
		resp.Diagnostics.AddError("Provider not configured", "connection resource used before Configure")
		return
	}
	var state resourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.svc.Delete(ctx, state.Vendor.ValueString(), state.ID.ValueString()); err != nil && !errors.Is(err, common.ErrNotFound) {
		resp.Diagnostics.AddError("Deleting connection", err.Error())
	}
}

// ImportState accepts "<vendor>/<connection_id>".
func (r *connectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitImportID(req.ID)
	if parts == nil {
		resp.Diagnostics.AddError(
			"Invalid import id",
			fmt.Sprintf("expected \"<vendor>/<connection_id>\", got %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vendor"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

// splitImportID validates "<vendor>/<connection_id>"; nil on malformed.
func splitImportID(id string) []string {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	return parts
}

// ModifyPlan validates the config against the tenant's live catalog at plan
// time (guardrails the practitioner doesn't have to write): the vendor must
// exist in this tenant's catalog. Skipped for destroys, unknown values, an
// unconfigured provider (validate-only runs), or validate_on_plan = false.
// Network errors downgrade to a warning — apply-time server enforcement is
// the backstop, a flaky plan is not acceptable.
func (r *connectionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy — nothing to mark or validate
	}
	var plan resourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The server recomputes credentials_fingerprint / status / updated_at when the
	// auth block changes; mark them unknown so UseStateForUnknown doesn't pin stale
	// values through the update. Must stay ahead of the validate_on_plan gate below:
	// skipping it leaves a known planned value that apply contradicts.
	if plan.Auth != nil && !req.State.Raw.IsNull() {
		var state resourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if !resp.Diagnostics.HasError() && authChanged(plan.Auth, state.Auth) {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("auth").AtName("credentials_fingerprint"), types.StringUnknown())...)
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("auth").AtName("status"), types.StringUnknown())...)
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("auth").AtName("updated_at"), types.StringUnknown())...)
		}
	}

	// Catalog-backed plan-time validation is best-effort and opt-out: skip for an
	// unconfigured provider (validate-only runs) or when validate_on_plan=false.
	if r.svc == nil || !r.validateOnPlan || plan.Vendor.IsNull() || plan.Vendor.IsUnknown() {
		return
	}

	vendorKey := plan.Vendor.ValueString()
	vendor, err := r.svc.GetVendor(ctx, vendorKey)
	switch {
	case err == nil:
	case errors.Is(err, common.ErrNotFound):
		resp.Diagnostics.AddAttributeError(path.Root("vendor"), "Unknown vendor",
			fmt.Sprintf("%q is not available in this tenant's integration catalog. "+
				"Check the spelling, or your licensing — the relyance_integration_vendor "+
				"data source shows what this tenant can connect.", vendorKey))
		return
	default:
		resp.Diagnostics.AddWarning("Could not validate vendor at plan time",
			fmt.Sprintf("catalog lookup for %q failed (%s); the apply will enforce it", vendorKey, err))
		return
	}

	if plan.Auth != nil {
		r.validateAuthPlan(ctx, req, resp, &plan, vendor)
	}
}

// validateAuthPlan runs the catalog-backed structural checks on the auth
// block, and — for connections that already exist — the server's
// side-effect-free semantic validation. Unknown values skip gracefully.
func (r *connectionResource) validateAuthPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
	plan *resourceModel,
	vendor *client.Vendor,
) {
	authPath := path.Root("auth")
	method := plan.Auth.Method
	if method.IsNull() || method.IsUnknown() {
		return
	}

	// Method must be one of the vendor's — match slug or legacy key.
	var matched *client.AuthConfig
	var available []string
	for i := range vendor.AuthConfigs {
		ac := &vendor.AuthConfigs[i]
		available = append(available, fmt.Sprintf("%s (%s)", ac.Slug, ac.DisplayName))
		if ac.Slug == method.ValueString() || ac.Key == method.ValueString() {
			matched = ac
		}
	}
	if matched == nil {
		resp.Diagnostics.AddAttributeError(authPath.AtName("method"), "Unknown authentication method",
			fmt.Sprintf("%q is not an authentication method of vendor %s. Available: %s",
				method.ValueString(), vendor.VendorKey, strings.Join(available, ", ")))
		return
	}

	// Field-key checks need known values.
	if plan.Auth.Params.IsUnknown() {
		return
	}
	known := map[string]bool{}
	secretField := map[string]bool{}
	for _, f := range matched.CustomFields {
		known[f.Key] = true
		secretField[f.Key] = f.IsThisSecret
	}
	var params map[string]string
	if !plan.Auth.Params.IsNull() {
		resp.Diagnostics.Append(plan.Auth.Params.ElementsAs(ctx, &params, false)...)
	}
	for k := range params {
		if !known[k] {
			resp.Diagnostics.AddAttributeError(authPath.AtName("params"), "Unknown auth field",
				fmt.Sprintf("field %q is not part of method %s for vendor %s", k, matched.Slug, vendor.VendorKey))
		} else if secretField[k] {
			resp.Diagnostics.AddAttributeError(authPath.AtName("params"), "Secret field in params",
				fmt.Sprintf("field %q is a secret — move it to auth.secrets_wo so it never lands in state", k))
		}
	}

	// Secrets come from CONFIG (write-only). Keys are checkable; values are
	// only sent, never diffed.
	var cfg resourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.Auth == nil {
		return
	}
	secrets := cfg.Auth.SecretsWO
	if secrets.IsUnknown() {
		return
	}
	var secretVals map[string]string
	if !secrets.IsNull() {
		resp.Diagnostics.Append(secrets.ElementsAs(ctx, &secretVals, false)...)
	}
	for k := range secretVals {
		if !known[k] {
			resp.Diagnostics.AddAttributeError(authPath.AtName("secrets_wo"), "Unknown auth field",
				fmt.Sprintf("field %q is not part of method %s for vendor %s", k, matched.Slug, vendor.VendorKey))
		} else if !secretField[k] {
			resp.Diagnostics.AddWarning("Non-secret field in secrets_wo",
				fmt.Sprintf("field %q is not marked secret in the catalog; keeping it in auth.secrets_wo works but hides it from plans", k))
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Server-side semantic validation — only possible for connections that
	// already exist (the endpoint is per-connection).
	if plan.ID.IsNull() || plan.ID.IsUnknown() {
		return
	}
	creds, mdiags := mergeAuthCreds(ctx, plan.Auth.Params, secrets)
	resp.Diagnostics.Append(mdiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	result, err := r.svc.ValidateAuth(ctx, plan.Vendor.ValueString(), plan.ID.ValueString(), client.AuthSaveRequest{
		AuthKey:     method.ValueString(),
		CustomCreds: creds,
	})
	if err != nil {
		resp.Diagnostics.AddWarning("Could not validate credentials at plan time",
			fmt.Sprintf("server validation failed to run (%s); the apply will enforce it", err))
		return
	}
	if !result.IsValid {
		detail := "credential validation failed"
		if result.Error != nil && *result.Error != "" {
			detail = *result.Error
		}
		for _, fr := range result.FieldResults {
			detail += fmt.Sprintf("\n  %v", fr)
		}
		resp.Diagnostics.AddAttributeError(authPath, "Invalid credential configuration", detail)
	}
}

// saveAuth issues PUT .../auth from the CONFIG's auth block (write-only
// secret values exist only there). Returns false on error.
func (r *connectionResource) saveAuth(ctx context.Context, config tfsdk.Config, plan *resourceModel, diags *diag.Diagnostics) bool {
	var cfg resourceModel
	diags.Append(config.Get(ctx, &cfg)...)
	if diags.HasError() {
		return false
	}
	var secrets types.Map
	if cfg.Auth != nil {
		secrets = cfg.Auth.SecretsWO
	}
	creds, mdiags := mergeAuthCreds(ctx, plan.Auth.Params, secrets)
	diags.Append(mdiags...)
	if diags.HasError() {
		return false
	}
	err := r.svc.SaveAuth(ctx, plan.Vendor.ValueString(), plan.ID.ValueString(), client.AuthSaveRequest{
		AuthKey:     plan.Auth.Method.ValueString(),
		CustomCreds: creds,
	})
	if err != nil {
		diags.AddAttributeError(path.Root("auth"), "Saving connection credentials", err.Error())
		return false
	}
	return true
}

// readBack refreshes the model from the server after a write; a failed
// read-back is surfaced but does not undo the write.
func (r *connectionResource) readBack(ctx context.Context, m *resourceModel) (ok bool, errText string) {
	detail, err := r.svc.Get(ctx, m.Vendor.ValueString(), m.ID.ValueString())
	if err != nil {
		return false, err.Error()
	}
	if diags := m.refreshFromAPI(ctx, detail); diags.HasError() {
		return false, "mapping server response to state"
	}
	m.refreshAuthComputed(detail)
	if diags := m.refreshScans(ctx, detail); diags.HasError() {
		return false, "mapping scans from server response"
	}
	if m.Auth != nil {
		// Write-only values must never be persisted to state.
		m.Auth.SecretsWO = types.MapNull(types.StringType)
	}
	return true, ""
}

func scalarsNonEmpty(s client.ScalarUpdateRequest) bool {
	return s.ConnectionName != nil || s.RefreshFrequency != nil || s.StartScanFrom != nil ||
		s.DataStorageLocation != nil || s.BusinessNodeIDs != nil || s.CredentialsExpireAt != nil ||
		s.RelyanceSecretAccess != nil
}
