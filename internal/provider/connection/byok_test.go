// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connection

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

const testARN = "arn:aws:secretsmanager:us-east-1:123456789012:secret:relyance/jira-AbCdEf"

// --- secret_ref validator ---------------------------------------------------

func resourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&connectionResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.Schema
}

func TestSecretRefSchema(t *testing.T) {
	s := resourceSchema(t)
	ref, ok := s.Attributes["secret_ref"].(schema.StringAttribute)
	if !ok || !ref.Optional || ref.Computed || ref.Sensitive || ref.Required {
		t.Fatalf("secret_ref must be Optional, not Computed/Sensitive/Required: %+v", s.Attributes["secret_ref"])
	}
	mode, ok := s.Attributes["runtime_mode"].(schema.StringAttribute)
	if !ok || !mode.Computed || !mode.Optional || mode.Required || len(mode.Validators) != 1 {
		t.Fatalf("runtime_mode must be Optional+Computed with a OneOf validator: %+v", s.Attributes["runtime_mode"])
	}
}

func TestSecretRefValidator(t *testing.T) {
	ref := resourceSchema(t).Attributes["secret_ref"].(schema.StringAttribute)
	if len(ref.Validators) != 1 {
		t.Fatalf("validators = %d, want 1", len(ref.Validators))
	}
	v := ref.Validators[0]

	cases := []struct {
		in    string
		valid bool
	}{
		{testARN, true},
		{"arn:aws:secretsmanager:eu-west-1:123456789012:secret:byok", true},
		{"arn:aws-us-gov:secretsmanager:us-gov-west-1:123456789012:secret:a/b_c+d=e.f@g-h", true},
		{"arn:aws-cn:secretsmanager:cn-north-1:123456789012:secret:x-1a2b3c", true},
		{"", false},
		{"arn:aws:secretsmanager:us-east-1:123456789012:secret:", false},     // no name
		{"arn:aws:secretsmanager:us-east-1:12345678901:secret:x", false},     // 11-digit account
		{"arn:aws:ssm:us-east-1:123456789012:parameter/x", false},            // wrong service
		{"arn:aws:secretsmanager:us-east-1:123456789012:x", false},           // no secret: segment
		{"arn:aws-eu:secretsmanager:us-east-1:123456789012:secret:x", false}, // unknown partition
		{"arn:aws:secretsmanager:us-east-1:123456789012:secret:x y", false},  // space
		{" " + testARN, false}, // leading space
		{"arn:aws:secretsmanager:US-EAST-1:123456789012:secret:x", false},          // region case
		{"arn:aws:secretsmanager:us-east-1:123456789012:secret:x#fragment", false}, // bad char
		{"arn:aws:secretsmanager:cn-north-1:123456789012:secret:x", false},         // aws + China region
		{"arn:aws:secretsmanager:us-gov-west-1:123456789012:secret:x", false},      // aws + GovCloud region
		{"arn:aws-cn:secretsmanager:us-east-1:123456789012:secret:x", false},       // aws-cn + commercial region
		{"arn:aws-us-gov:secretsmanager:us-east-1:123456789012:secret:x", false},   // aws-us-gov + commercial region
		{"arn:aws-cn:secretsmanager:cn-northwest-1:123456789012:secret:x", true},
		{"arn:aws-us-gov:secretsmanager:us-gov-east-1:123456789012:secret:x", true},
	}
	for _, tc := range cases {
		req := validator.StringRequest{Path: path.Root("secret_ref"), ConfigValue: types.StringValue(tc.in)}
		var resp validator.StringResponse
		v.ValidateString(context.Background(), req, &resp)
		if got := !resp.Diagnostics.HasError(); got != tc.valid {
			t.Errorf("%q: valid = %v, want %v (%v)", tc.in, got, tc.valid, resp.Diagnostics)
		}
	}

	// Null and unknown are left to the other rules.
	for _, val := range []types.String{types.StringNull(), types.StringUnknown()} {
		var resp validator.StringResponse
		v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("secret_ref"), ConfigValue: val}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%v: unexpected error %v", val, resp.Diagnostics)
		}
	}
}

// --- config-only rules --------------------------------------------------------

func hasErrorAt(diags diag.Diagnostics, p path.Path) bool {
	for _, d := range diags.Errors() {
		if wp, ok := d.(diag.DiagnosticWithPath); ok && wp.Path().Equal(p) {
			return true
		}
	}
	return false
}

func TestValidateSecretRefConfig(t *testing.T) {
	ctx := context.Background()
	authWith := func(secrets types.Map) *authModel {
		return &authModel{Method: types.StringValue("api-key"), Params: types.MapNull(types.StringType), SecretsWO: secrets}
	}

	cases := []struct {
		name    string
		cfg     resourceModel
		errPath *path.Path
	}{
		{"no secret_ref, no auth", resourceModel{SecretRef: types.StringNull()}, nil},
		{"unknown secret_ref skipped", resourceModel{SecretRef: types.StringUnknown()}, nil},
		{"secret_ref without auth", resourceModel{SecretRef: types.StringValue(testARN)}, ptr(path.Root("secret_ref"))},
		{"secret_ref with method only", resourceModel{SecretRef: types.StringValue(testARN), Auth: authWith(types.MapNull(types.StringType))}, nil},
		{"secret_ref with empty secrets_wo", resourceModel{SecretRef: types.StringValue(testARN), Auth: authWith(strMap(t, map[string]string{}))}, nil},
		{"secret_ref with unknown secrets_wo", resourceModel{SecretRef: types.StringValue(testARN), Auth: authWith(types.MapUnknown(types.StringType))}, nil},
		{
			"secret_ref with secrets_wo",
			resourceModel{SecretRef: types.StringValue(testARN), Auth: authWith(strMap(t, map[string]string{"API_KEY": "x"}))},
			ptr(path.Root("auth").AtName("secrets_wo")),
		},
		{"secrets_wo without secret_ref is fine here", resourceModel{SecretRef: types.StringNull(), Auth: authWith(strMap(t, map[string]string{"API_KEY": "x"}))}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateSecretRefConfig(ctx, &tc.cfg)
			if tc.errPath == nil {
				if diags.HasError() {
					t.Fatalf("unexpected: %v", diags)
				}
				return
			}
			if !hasErrorAt(diags, *tc.errPath) {
				t.Fatalf("want error at %s, got %v", tc.errPath, diags)
			}
		})
	}
}

func TestValidateSecretRefConfigNeverEchoesSecretValues(t *testing.T) {
	cfg := resourceModel{
		SecretRef: types.StringValue(testARN),
		Auth: &authModel{
			Method:    types.StringValue("api-key"),
			SecretsWO: strMap(t, map[string]string{"API_KEY": "super-secret-value"}),
		},
	}
	for _, d := range validateSecretRefConfig(context.Background(), &cfg) {
		if strings.Contains(d.Detail(), "super-secret-value") || strings.Contains(d.Summary(), "super-secret-value") {
			t.Fatalf("diagnostic leaks a secret value: %s", d.Detail())
		}
	}
}

// --- plan rules (state, no network) -------------------------------------------

func byokState(mode string, ref types.String) *resourceModel {
	return &resourceModel{RuntimeMode: types.StringValue(mode), SecretRef: ref}
}

func TestTargetRuntimeMode(t *testing.T) {
	byok, hosted := client.RuntimeModeInHostBYOK, client.RuntimeModeRelyanceHosted
	cases := []struct {
		name      string
		cfg       types.String
		state     *resourceModel
		wantMode  string
		wantKnown bool
	}{
		{"configured wins over state", types.StringValue(byok), byokState(hosted, types.StringNull()), byok, true},
		{"configured on create", types.StringValue(byok), nil, byok, true},
		{"configured but not yet known", types.StringUnknown(), nil, "", false},
		{"unmanaged: current value", types.StringNull(), byokState(byok, types.StringNull()), byok, true},
		{"unmanaged on create: server default", types.StringNull(), nil, hosted, true},
		{"unmanaged, state unknown", types.StringNull(), &resourceModel{RuntimeMode: types.StringNull()}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, known := targetRuntimeMode(tc.cfg, tc.state)
			if mode != tc.wantMode || known != tc.wantKnown {
				t.Fatalf("got (%q, %v), want (%q, %v)", mode, known, tc.wantMode, tc.wantKnown)
			}
		})
	}
}

func TestByokPlanDiags(t *testing.T) {
	ctx := context.Background()
	withAuth := &authModel{Method: types.StringValue("api-key")}
	secrets := strMap(t, map[string]string{"API_KEY": "x"})
	nullMap := types.MapNull(types.StringType)
	byok, hosted := client.RuntimeModeInHostBYOK, client.RuntimeModeRelyanceHosted

	cases := []struct {
		name    string
		plan    resourceModel
		mode    string
		known   bool
		secrets types.Map
		errPath *path.Path
	}{
		{"no secret_ref", resourceModel{SecretRef: types.StringNull()}, hosted, true, nullMap, nil},
		{"secret_ref on BYOK", resourceModel{SecretRef: types.StringValue(testARN), Auth: withAuth}, byok, true, nullMap, nil},
		{"not-yet-known secret_ref on BYOK", resourceModel{SecretRef: types.StringUnknown(), Auth: withAuth}, byok, true, nullMap, nil},
		{"secret_ref on hosted", resourceModel{SecretRef: types.StringValue(testARN), Auth: withAuth}, hosted, true, nullMap, ptr(path.Root("secret_ref"))},
		{"not-yet-known secret_ref on hosted", resourceModel{SecretRef: types.StringUnknown(), Auth: withAuth}, hosted, true, nullMap, ptr(path.Root("secret_ref"))},
		{"secret_ref on IN_HOST", resourceModel{SecretRef: types.StringValue(testARN), Auth: withAuth}, client.RuntimeModeInHost, true, nullMap, ptr(path.Root("secret_ref"))},
		{"unknown mode is not judged", resourceModel{SecretRef: types.StringValue(testARN), Auth: withAuth}, "", false, secrets, nil},
		{"BYOK with secrets_wo", resourceModel{SecretRef: types.StringNull(), Auth: withAuth}, byok, true, secrets, ptr(path.Root("auth").AtName("secrets_wo"))},
		{"hosted with secrets_wo is unchanged behaviour", resourceModel{SecretRef: types.StringNull(), Auth: withAuth}, hosted, true, secrets, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := byokPlanDiags(ctx, &tc.plan, tc.mode, tc.known, tc.secrets)
			if tc.errPath == nil {
				if diags.HasError() {
					t.Fatalf("unexpected: %v", diags)
				}
				return
			}
			if !hasErrorAt(diags, *tc.errPath) {
				t.Fatalf("want error at %s, got %v", tc.errPath, diags)
			}
		})
	}
}

func TestByokParamDiags(t *testing.T) {
	matched := &client.AuthConfig{Slug: "api-key", CustomFields: []client.CustomField{
		{Key: "data_storage_location", IsTopLevel: true},
		{Key: "region_hint", IsTopLevelSnake: true},
		{Key: "ORG_ID", IsThisSecret: true},
		{Key: "base_url"},
	}}
	if d := byokParamDiags(map[string]string{"data_storage_location": "us", "region_hint": "x", "not_a_field": "y"}, matched, "atlassian_jira"); d.HasError() {
		t.Fatalf("top-level (both spellings) and unknown keys must pass here: %v", d)
	}
	d := byokParamDiags(map[string]string{"base_url": "https://example.com", "ORG_ID": "o", "data_storage_location": "us"}, matched, "atlassian_jira")
	if n := d.ErrorsCount(); n != 2 {
		t.Fatalf("errors = %d, want 2 (base_url, ORG_ID): %v", n, d)
	}
	for _, e := range d.Errors() {
		if strings.Contains(e.Detail(), "https://example.com") {
			t.Fatalf("diagnostic echoes a field value: %s", e.Detail())
		}
	}
}

// --- secret_ref wire value ----------------------------------------------------

func TestSecretRefWire(t *testing.T) {
	other := "arn:aws:secretsmanager:us-east-1:123456789012:secret:other"
	cases := []struct {
		name  string
		plan  types.String
		state *resourceModel
		want  *string
	}{
		{"create, unset", types.StringNull(), nil, nil},
		{"create, set", types.StringValue(testARN), nil, ptr(testARN)},
		{"unknown sends nothing", types.StringUnknown(), byokState(client.RuntimeModeInHostBYOK, types.StringValue(testARN)), nil},
		{"unchanged sends nothing", types.StringValue(testARN), byokState(client.RuntimeModeInHostBYOK, types.StringValue(testARN)), nil},
		{"changed sends new", types.StringValue(other), byokState(client.RuntimeModeInHostBYOK, types.StringValue(testARN)), ptr(other)},
		{"added sends new", types.StringValue(testARN), byokState(client.RuntimeModeInHostBYOK, types.StringNull()), ptr(testARN)},
		{"removed sends empty (clear)", types.StringNull(), byokState(client.RuntimeModeInHostBYOK, types.StringValue(testARN)), ptr("")},
		{"never set sends nothing", types.StringNull(), byokState(client.RuntimeModeInHostBYOK, types.StringNull()), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := secretRefWire(tc.plan, tc.state, client.RuntimeModeInHostBYOK)
			switch {
			case got == nil && tc.want == nil:
			case got == nil || tc.want == nil || *got != *tc.want:
				t.Fatalf("got %v, want %v", deref(got), deref(tc.want))
			}
		})
	}
}

func TestSecretRefWireOffBYOKSendsNothing(t *testing.T) {
	// Switching away from BYOK: the server clears secret_ref itself and rejects
	// secretRef on other modes, so nothing is sent.
	state := byokState(client.RuntimeModeInHostBYOK, types.StringValue(testARN))
	for _, mode := range []string{client.RuntimeModeRelyanceHosted, client.RuntimeModeInHost, client.RuntimeModeInHome, ""} {
		if got := secretRefWire(types.StringNull(), state, mode); got != nil {
			t.Fatalf("mode %q: got %q, want nil", mode, *got)
		}
	}
}

// --- read mapping -------------------------------------------------------------

func TestRefreshFromAPIRuntimeModeAndSecretRef(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		conn     map[string]any
		wantMode string
		wantRef  types.String
	}{
		{"absent runtime_mode defaults", map[string]any{}, client.RuntimeModeRelyanceHosted, types.StringNull()},
		{"BYOK with ref", map[string]any{"runtime_mode": "IN_HOST_BYOK", "auth": map[string]any{"secret_ref": testARN}}, "IN_HOST_BYOK", types.StringValue(testARN)},
		{"BYOK null ref", map[string]any{"runtime_mode": "IN_HOST_BYOK", "auth": map[string]any{"secret_ref": nil}}, "IN_HOST_BYOK", types.StringNull()},
		{"empty ref is null", map[string]any{"runtime_mode": "IN_HOST_BYOK", "auth": map[string]any{"secret_ref": ""}}, "IN_HOST_BYOK", types.StringNull()},
		{"in-host", map[string]any{"runtime_mode": "IN_HOST"}, "IN_HOST", types.StringNull()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A previously-set ref must be overwritten by server truth (drift).
			m := resourceModel{
				SecretRef:           types.StringValue("stale"),
				RefreshFrequency:    types.Int64Null(),
				ScanFrom:            types.StringNull(),
				BusinessNodeIDs:     types.SetNull(types.StringType),
				SupportSecretAccess: types.BoolNull(),
			}
			if d := m.refreshFromAPI(ctx, &client.ConnectionDetail{Connection: tc.conn}); d.HasError() {
				t.Fatal(d)
			}
			if m.RuntimeMode.ValueString() != tc.wantMode {
				t.Fatalf("runtime_mode = %v, want %s", m.RuntimeMode, tc.wantMode)
			}
			if !m.SecretRef.Equal(tc.wantRef) {
				t.Fatalf("secret_ref = %v, want %v", m.SecretRef, tc.wantRef)
			}
		})
	}
}

// --- apply helpers ------------------------------------------------------------

func TestSaveAuthWithoutAuthBlockUsesServerMethod(t *testing.T) {
	f := &fakeService{detail: &client.ConnectionDetail{Connection: map[string]any{
		"runtime_mode": "IN_HOST_BYOK",
		"auth":         map[string]any{"type": "AUTH_TYPE_API_KEY", "secret_ref": testARN},
	}}}
	r := &connectionResource{svc: f}
	plan := &resourceModel{Vendor: types.StringValue("atlassian_jira"), ID: types.StringValue("3")}
	var diags diag.Diagnostics
	if !r.saveAuth(context.Background(), tfsdk.Config{}, plan, ptr(""), &diags) {
		t.Fatal(diags)
	}
	if len(f.saveReqs) != 1 {
		t.Fatalf("save calls = %d", len(f.saveReqs))
	}
	got := f.saveReqs[0]
	if got.AuthKey != "AUTH_TYPE_API_KEY" || got.SecretRef == nil || *got.SecretRef != "" || got.CustomCreds != nil {
		t.Fatalf("save req = %+v", got)
	}
}

func TestSaveAuthWithoutAuthMethodFails(t *testing.T) {
	f := &fakeService{detail: &client.ConnectionDetail{Connection: map[string]any{"auth": map[string]any{}}}}
	r := &connectionResource{svc: f}
	plan := &resourceModel{Vendor: types.StringValue("atlassian_jira"), ID: types.StringValue("3")}
	var diags diag.Diagnostics
	if r.saveAuth(context.Background(), tfsdk.Config{}, plan, ptr(""), &diags) || !diags.HasError() {
		t.Fatal("expected failure without an auth method")
	}
	if len(f.saveReqs) != 0 {
		t.Fatal("must not save")
	}
}

func TestReadBackDetectsUnstoredSecretRef(t *testing.T) {
	f := &fakeService{detail: &client.ConnectionDetail{Connection: map[string]any{"runtime_mode": "IN_HOST_BYOK", "auth": map[string]any{}}}}
	r := &connectionResource{svc: f}
	m := &resourceModel{
		Vendor: types.StringValue("atlassian_jira"), ID: types.StringValue("3"),
		SecretRef: types.StringValue(testARN), Scans: types.MapNull(scanObjectType()),
		RefreshFrequency: types.Int64Null(), ScanFrom: types.StringNull(),
		BusinessNodeIDs: types.SetNull(types.StringType), SupportSecretAccess: types.BoolNull(),
	}
	ok, errText := r.readBack(context.Background(), m)
	if ok || !strings.Contains(errText, "did not store secret_ref") {
		t.Fatalf("ok=%v err=%q", ok, errText)
	}

	f.detail.Connection["auth"] = map[string]any{"secret_ref": testARN}
	if ok, errText := r.readBack(context.Background(), m); !ok {
		t.Fatalf("stored ref must pass: %s", errText)
	}
}

// --- framework wiring (ValidateConfig / ModifyPlan / Update) -------------------

// tfObject builds a value of the schema's object type, null for every attribute
// not given.
func tfObject(typ tftypes.Object, vals map[string]tftypes.Value) tftypes.Value {
	m := map[string]tftypes.Value{}
	for name, at := range typ.AttributeTypes {
		if v, ok := vals[name]; ok {
			m[name] = v
		} else {
			m[name] = tftypes.NewValue(at, nil)
		}
	}
	return tftypes.NewValue(typ, m)
}

type tfHarness struct {
	t      *testing.T
	schema schema.Schema
	root   tftypes.Object
	auth   tftypes.Object
}

func newHarness(t *testing.T) *tfHarness {
	s := resourceSchema(t)
	root := s.Type().TerraformType(context.Background()).(tftypes.Object)
	return &tfHarness{t: t, schema: s, root: root, auth: root.AttributeTypes["auth"].(tftypes.Object)}
}

func str(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

func (h *tfHarness) strMapVal(m map[string]string) tftypes.Value {
	vals := map[string]tftypes.Value{}
	for k, v := range m {
		vals[k] = str(v)
	}
	return tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, vals)
}

// authVal builds the auth object; secrets go in CONFIG only (write-only).
func (h *tfHarness) authVal(params, secrets map[string]string) tftypes.Value {
	vals := map[string]tftypes.Value{"method": str("api-key")}
	if params != nil {
		vals["params"] = h.strMapVal(params)
	}
	if secrets != nil {
		vals["secrets_wo"] = h.strMapVal(secrets)
	}
	return tfObject(h.auth, vals)
}

func (h *tfHarness) obj(vals map[string]tftypes.Value) tftypes.Value {
	base := map[string]tftypes.Value{"vendor": str("atlassian_jira"), "name": str("Jira")}
	for k, v := range vals {
		base[k] = v
	}
	return tfObject(h.root, base)
}

func (h *tfHarness) stateVals(mode string, ref *string, withAuth bool) map[string]tftypes.Value {
	vals := map[string]tftypes.Value{"id": str("3"), "runtime_mode": str(mode), "integration_type": str("INTEGRATION_TYPE_VENDOR")}
	if ref != nil {
		vals["secret_ref"] = str(*ref)
	}
	if withAuth {
		vals["auth"] = tfObject(h.auth, map[string]tftypes.Value{
			"method": str("api-key"), "status": str("AUTH_STATUS_CONNECTED"),
			"credentials_fingerprint": str("fp"), "updated_at": str("2026-09-01T00:00:00Z"),
		})
	}
	return vals
}

func (h *tfHarness) modifyPlan(r *connectionResource, cfg, plan, state tftypes.Value) *resource.ModifyPlanResponse {
	req := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: h.schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.schema, Raw: plan},
		State:  tfsdk.State{Schema: h.schema, Raw: state},
	}
	resp := &resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(context.Background(), req, resp)
	return resp
}

func TestValidateConfigWiring(t *testing.T) {
	h := newHarness(t)
	r := &connectionResource{}
	run := func(cfg tftypes.Value) diag.Diagnostics {
		resp := &resource.ValidateConfigResponse{}
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: h.schema, Raw: cfg}}, resp)
		return resp.Diagnostics
	}
	if d := run(h.obj(map[string]tftypes.Value{"auth": h.authVal(map[string]string{"data_storage_location": "us"}, nil), "secret_ref": str(testARN)})); d.HasError() {
		t.Fatalf("valid BYOK config: %v", d)
	}
	if d := run(h.obj(map[string]tftypes.Value{"secret_ref": str(testARN)})); !hasErrorAt(d, path.Root("secret_ref")) {
		t.Fatalf("secret_ref without auth: %v", d)
	}
	if d := run(h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, map[string]string{"API_KEY": "x"}), "secret_ref": str(testARN)})); !hasErrorAt(d, path.Root("auth").AtName("secrets_wo")) {
		t.Fatalf("secret_ref with secrets_wo: %v", d)
	}
	if d := run(h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, map[string]string{"API_KEY": "x"})})); d.HasError() {
		t.Fatalf("non-BYOK config must be unchanged: %v", d)
	}
}

func TestModifyPlanWiring(t *testing.T) {
	h := newHarness(t)

	t.Run("create with secret_ref errors", func(t *testing.T) {
		v := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil), "secret_ref": str(testARN)})
		resp := h.modifyPlan(&connectionResource{}, v, v, tftypes.NewValue(h.root, nil))
		if !hasErrorAt(resp.Diagnostics, path.Root("secret_ref")) {
			t.Fatalf("diags = %v", resp.Diagnostics)
		}
	})

	t.Run("create with runtime_mode BYOK and secret_ref is allowed", func(t *testing.T) {
		v := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil), "secret_ref": str(testARN), "runtime_mode": str(client.RuntimeModeInHostBYOK)})
		resp := h.modifyPlan(&connectionResource{}, v, v, tftypes.NewValue(h.root, nil))
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
	})

	t.Run("create with runtime_mode BYOK rejects secrets_wo", func(t *testing.T) {
		cfg := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, map[string]string{"API_KEY": "x"}), "runtime_mode": str(client.RuntimeModeInHostBYOK)})
		plan := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil), "runtime_mode": str(client.RuntimeModeInHostBYOK)})
		resp := h.modifyPlan(&connectionResource{}, cfg, plan, tftypes.NewValue(h.root, nil))
		if !hasErrorAt(resp.Diagnostics, path.Root("auth").AtName("secrets_wo")) {
			t.Fatalf("diags = %v", resp.Diagnostics)
		}
	})

	t.Run("switching away from BYOK with secret_ref still set errors", func(t *testing.T) {
		state := h.obj(h.stateVals(client.RuntimeModeInHostBYOK, ptr(testARN), true))
		planVals := h.stateVals(client.RuntimeModeRelyanceHosted, ptr(testARN), true)
		cfg := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil), "secret_ref": str(testARN), "runtime_mode": str(client.RuntimeModeRelyanceHosted)})
		resp := h.modifyPlan(&connectionResource{}, cfg, h.obj(planVals), state)
		if !hasErrorAt(resp.Diagnostics, path.Root("secret_ref")) {
			t.Fatalf("diags = %v", resp.Diagnostics)
		}
	})

	t.Run("hosted connection with secret_ref errors", func(t *testing.T) {
		state := h.obj(h.stateVals(client.RuntimeModeRelyanceHosted, nil, true))
		vals := h.stateVals(client.RuntimeModeRelyanceHosted, ptr(testARN), true)
		plan := h.obj(vals)
		cfg := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil), "secret_ref": str(testARN)})
		resp := h.modifyPlan(&connectionResource{}, cfg, plan, state)
		if !hasErrorAt(resp.Diagnostics, path.Root("secret_ref")) {
			t.Fatalf("diags = %v", resp.Diagnostics)
		}
	})

	t.Run("BYOK adding secret_ref plans an auth save", func(t *testing.T) {
		state := h.obj(h.stateVals(client.RuntimeModeInHostBYOK, nil, true))
		plan := h.obj(h.stateVals(client.RuntimeModeInHostBYOK, ptr(testARN), true))
		cfg := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil), "secret_ref": str(testARN)})
		resp := h.modifyPlan(&connectionResource{}, cfg, plan, state)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		var fp types.String
		resp.Plan.GetAttribute(context.Background(), path.Root("auth").AtName("credentials_fingerprint"), &fp)
		if !fp.IsUnknown() {
			t.Fatalf("credentials_fingerprint should be unknown after a secret_ref change, got %v", fp)
		}
	})

	t.Run("BYOK with secrets_wo errors without network", func(t *testing.T) {
		state := h.obj(h.stateVals(client.RuntimeModeInHostBYOK, nil, true))
		plan := h.obj(h.stateVals(client.RuntimeModeInHostBYOK, nil, true))
		cfg := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, map[string]string{"API_KEY": "x"})})
		resp := h.modifyPlan(&connectionResource{}, cfg, plan, state)
		if !hasErrorAt(resp.Diagnostics, path.Root("auth").AtName("secrets_wo")) {
			t.Fatalf("diags = %v", resp.Diagnostics)
		}
	})

	t.Run("hosted with secrets_wo is unchanged", func(t *testing.T) {
		state := h.obj(h.stateVals(client.RuntimeModeRelyanceHosted, nil, true))
		plan := h.obj(h.stateVals(client.RuntimeModeRelyanceHosted, nil, true))
		cfg := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, map[string]string{"API_KEY": "x"})})
		resp := h.modifyPlan(&connectionResource{}, cfg, plan, state)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
	})

	t.Run("BYOK catalog check rejects non-top-level params and validates secret_ref", func(t *testing.T) {
		f := &fakeService{vendor: &client.Vendor{VendorKey: "atlassian_jira", AuthConfigs: []client.AuthConfig{{
			Slug: "api-key", Key: "AUTH_TYPE_API_KEY",
			CustomFields: []client.CustomField{
				{Key: "data_storage_location", IsTopLevel: true},
				{Key: "ORG_ID", IsThisSecret: true},
				{Key: "API_KEY", IsThisSecret: true},
			},
		}}}}
		r := &connectionResource{svc: f, validateOnPlan: true}

		stateVals := h.stateVals(client.RuntimeModeInHostBYOK, ptr(testARN), true)
		state := h.obj(stateVals)

		ok := h.authVal(map[string]string{"data_storage_location": "us"}, nil)
		planVals := h.stateVals(client.RuntimeModeInHostBYOK, ptr(testARN), false)
		planVals["auth"] = ok
		resp := h.modifyPlan(r, h.obj(map[string]tftypes.Value{"auth": ok, "secret_ref": str(testARN)}), h.obj(planVals), state)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		if n := len(f.validateReqs); n != 1 || f.validateReqs[0].SecretRef == nil || *f.validateReqs[0].SecretRef != testARN {
			t.Fatalf("validate reqs = %+v", f.validateReqs)
		}

		bad := h.authVal(map[string]string{"data_storage_location": "us", "ORG_ID": "o"}, nil)
		planVals["auth"] = bad
		resp = h.modifyPlan(r, h.obj(map[string]tftypes.Value{"auth": bad, "secret_ref": str(testARN)}), h.obj(planVals), state)
		if !hasErrorAt(resp.Diagnostics, path.Root("auth").AtName("params")) {
			t.Fatalf("diags = %v", resp.Diagnostics)
		}
		for _, d := range resp.Diagnostics.Errors() {
			if strings.Contains(d.Summary(), "Secret field in params") {
				t.Fatalf("BYOK should report the BYOK error, not the generic one: %v", d)
			}
		}
	})
}

func TestUpdateClearsRemovedSecretRef(t *testing.T) {
	h := newHarness(t)
	f := &fakeService{detail: &client.ConnectionDetail{Connection: map[string]any{
		"connection_name": "Jira", "integrationType": "INTEGRATION_TYPE_VENDOR", "runtime_mode": "IN_HOST_BYOK",
		"auth": map[string]any{"type": "AUTH_TYPE_API_KEY", "status": "AUTH_STATUS_CONNECTED"},
	}}}
	r := &connectionResource{svc: f}

	state := h.obj(h.stateVals(client.RuntimeModeInHostBYOK, ptr(testARN), true))
	plan := h.obj(h.stateVals(client.RuntimeModeInHostBYOK, nil, true))
	cfg := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil)})
	req := resource.UpdateRequest{
		Config: tfsdk.Config{Schema: h.schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.schema, Raw: plan},
		State:  tfsdk.State{Schema: h.schema, Raw: state},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: h.schema, Raw: state}}
	r.Update(context.Background(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(f.saveReqs) != 1 || f.saveReqs[0].SecretRef == nil || *f.saveReqs[0].SecretRef != "" || f.saveReqs[0].AuthKey != "api-key" {
		t.Fatalf("save reqs = %+v", f.saveReqs)
	}
	var ref types.String
	resp.State.GetAttribute(context.Background(), path.Root("secret_ref"), &ref)
	if !ref.IsNull() {
		t.Fatalf("secret_ref after clear = %v", ref)
	}
}

func TestUpdateHostedConnectionSendsNoSecretRef(t *testing.T) {
	h := newHarness(t)
	f := &fakeService{detail: &client.ConnectionDetail{Connection: map[string]any{
		"connection_name": "Jira", "integrationType": "INTEGRATION_TYPE_VENDOR",
		"auth": map[string]any{"type": "AUTH_TYPE_API_KEY"},
	}}}
	r := &connectionResource{svc: f}
	stateVals := h.stateVals(client.RuntimeModeRelyanceHosted, nil, true)
	state := h.obj(stateVals)
	stateVals["name"] = str("Jira renamed")
	plan := h.obj(stateVals)
	req := resource.UpdateRequest{
		Config: tfsdk.Config{Schema: h.schema, Raw: h.obj(map[string]tftypes.Value{"name": str("Jira renamed"), "auth": h.authVal(nil, nil)})},
		Plan:   tfsdk.Plan{Schema: h.schema, Raw: plan},
		State:  tfsdk.State{Schema: h.schema, Raw: state},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: h.schema, Raw: state}}
	r.Update(context.Background(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(f.saveReqs) != 0 {
		t.Fatalf("a rename must not save auth: %+v", f.saveReqs)
	}
}

// createHarness runs Create for a config and returns the fake's calls.
func runCreate(t *testing.T, h *tfHarness, f *fakeService, cfgVals map[string]tftypes.Value) *resource.CreateResponse {
	t.Helper()
	cfg := h.obj(cfgVals)
	planVals := map[string]tftypes.Value{}
	for k, v := range cfgVals {
		planVals[k] = v
	}
	planVals["id"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	if _, ok := cfgVals["runtime_mode"]; !ok {
		planVals["runtime_mode"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	}
	req := resource.CreateRequest{
		Config: tfsdk.Config{Schema: h.schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.schema, Raw: h.obj(planVals)},
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: h.schema, Raw: tftypes.NewValue(h.root, nil)}}
	(&connectionResource{svc: f}).Create(context.Background(), req, resp)
	return resp
}

func TestCreateBYOKInOneApply(t *testing.T) {
	h := newHarness(t)
	f := &fakeService{created: "7", detail: &client.ConnectionDetail{Connection: map[string]any{
		"connection_name": "Jira", "integrationType": "INTEGRATION_TYPE_VENDOR", "runtime_mode": "IN_HOST_BYOK",
		"auth": map[string]any{"type": "AUTH_TYPE_API_KEY", "status": "AUTH_STATUS_CONNECTED", "secret_ref": testARN},
	}}}
	resp := runCreate(t, h, f, map[string]tftypes.Value{
		"auth":         h.authVal(map[string]string{"data_storage_location": "us"}, nil),
		"runtime_mode": str(client.RuntimeModeInHostBYOK),
		"secret_ref":   str(testARN),
	})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	// Order: create, runtime_mode PATCH, auth save (with secretRef), read-back.
	want := []string{"create atlassian_jira Jira", "patch atlassian_jira/7", "saveauth atlassian_jira/7 api-key", "get atlassian_jira/7"}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	if m := f.scalarReqs[0].RuntimeMode; m == nil || *m != client.RuntimeModeInHostBYOK || f.scalarReqs[0].ConnectionName != nil {
		t.Fatalf("runtime PATCH = %+v", f.scalarReqs[0])
	}
	if r := f.saveReqs[0].SecretRef; r == nil || *r != testARN {
		t.Fatalf("save req = %+v", f.saveReqs[0])
	}
	var mode, ref types.String
	resp.State.GetAttribute(context.Background(), path.Root("runtime_mode"), &mode)
	resp.State.GetAttribute(context.Background(), path.Root("secret_ref"), &ref)
	if mode.ValueString() != client.RuntimeModeInHostBYOK || ref.ValueString() != testARN {
		t.Fatalf("state runtime_mode=%v secret_ref=%v", mode, ref)
	}
}

func TestCreateWithoutRuntimeModeSendsNoRuntimePatch(t *testing.T) {
	h := newHarness(t)
	f := &fakeService{created: "7", detail: &client.ConnectionDetail{Connection: map[string]any{
		"connection_name": "Jira", "integrationType": "INTEGRATION_TYPE_VENDOR",
	}}}
	resp := runCreate(t, h, f, map[string]tftypes.Value{})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(f.scalarReqs) != 0 {
		t.Fatalf("unexpected PATCH: %+v", f.scalarReqs)
	}
	var mode types.String
	resp.State.GetAttribute(context.Background(), path.Root("runtime_mode"), &mode)
	if mode.ValueString() != client.RuntimeModeRelyanceHosted {
		t.Fatalf("runtime_mode = %v", mode)
	}
}

func TestRuntimeMode422IsAClearDiagnostic(t *testing.T) {
	h := newHarness(t)
	f := &fakeService{created: "7", scalarErr: fmt.Errorf("PATCH /x -> HTTP 422 Unprocessable Entity: tenant has no InHost deployment")}
	resp := runCreate(t, h, f, map[string]tftypes.Value{"runtime_mode": str(client.RuntimeModeInHostBYOK)})
	if !hasErrorAt(resp.Diagnostics, path.Root("runtime_mode")) {
		t.Fatalf("diags = %v", resp.Diagnostics)
	}
	d := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(d, "no InHost deployment") || !strings.Contains(d, "enabled deployment") {
		t.Fatalf("detail = %s", d)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "saveauth") {
			t.Fatal("auth must not be saved after a failed runtime_mode PATCH")
		}
	}
}

func TestSecretRef422IsAClearDiagnostic(t *testing.T) {
	f := &fakeService{saveErr: fmt.Errorf("PUT /x -> HTTP 422 Unprocessable Entity: Secret references are available only for Outpost on AWS.")}
	r := &connectionResource{svc: f}
	plan := &resourceModel{Vendor: types.StringValue("atlassian_jira"), ID: types.StringValue("3"),
		Auth: &authModel{Method: types.StringValue("api-key"), Params: types.MapNull(types.StringType)}}
	h := newHarness(t)
	cfg := tfsdk.Config{Schema: h.schema, Raw: h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil)})}
	var diags diag.Diagnostics
	if r.saveAuth(context.Background(), cfg, plan, ptr(testARN), &diags) {
		t.Fatal("expected failure")
	}
	if !hasErrorAt(diags, path.Root("secret_ref")) || !strings.Contains(diags.Errors()[0].Detail(), "Outpost on AWS") {
		t.Fatalf("diags = %v", diags)
	}
}

func TestUpdateSwitchAwayFromBYOK(t *testing.T) {
	h := newHarness(t)
	f := &fakeService{detail: &client.ConnectionDetail{Connection: map[string]any{
		"connection_name": "Jira", "integrationType": "INTEGRATION_TYPE_VENDOR", "runtime_mode": "RELYANCE_HOSTED",
		"auth": map[string]any{"type": "AUTH_TYPE_API_KEY"},
	}}}
	r := &connectionResource{svc: f}
	state := h.obj(h.stateVals(client.RuntimeModeInHostBYOK, ptr(testARN), true))
	plan := h.obj(h.stateVals(client.RuntimeModeRelyanceHosted, nil, true))
	cfg := h.obj(map[string]tftypes.Value{"auth": h.authVal(nil, nil), "runtime_mode": str(client.RuntimeModeRelyanceHosted)})
	req := resource.UpdateRequest{
		Config: tfsdk.Config{Schema: h.schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.schema, Raw: plan},
		State:  tfsdk.State{Schema: h.schema, Raw: state},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: h.schema, Raw: state}}
	r.Update(context.Background(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	// First PATCH is runtime_mode alone; the usual scalar PATCH follows.
	if len(f.scalarReqs) == 0 || f.scalarReqs[0].RuntimeMode == nil || *f.scalarReqs[0].RuntimeMode != client.RuntimeModeRelyanceHosted ||
		f.scalarReqs[0].ConnectionName != nil {
		t.Fatalf("PATCH reqs = %+v", f.scalarReqs)
	}
	for _, r := range f.scalarReqs[1:] {
		if r.RuntimeMode != nil {
			t.Fatalf("runtime_mode sent twice: %+v", f.scalarReqs)
		}
	}
	if len(f.saveReqs) != 0 {
		t.Fatalf("no auth save expected (the server clears secret_ref): %+v", f.saveReqs)
	}
}

func ptr[T any](v T) *T { return &v }

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
