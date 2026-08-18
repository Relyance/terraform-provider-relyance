// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connection

import (
	"context"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

// --- mergeAuthCreds -------------------------------------------------------
//
// mergeAuthCreds unions auth.params and auth.secrets_wo into the single
// customCreds map the API takes. These cases extend the "union" / "collision"
// / "both null" cases already in model_test.go with unknown maps and mixed
// null/non-null combinations.

func TestMergeAuthCredsTableDriven(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		params     types.Map
		secrets    types.Map
		wantErr    bool
		wantResult map[string]any
	}{
		{
			name:       "both unknown -> empty, no error",
			params:     types.MapUnknown(types.StringType),
			secrets:    types.MapUnknown(types.StringType),
			wantErr:    false,
			wantResult: map[string]any{},
		},
		{
			name:       "params unknown, secrets null -> empty",
			params:     types.MapUnknown(types.StringType),
			secrets:    types.MapNull(types.StringType),
			wantErr:    false,
			wantResult: map[string]any{},
		},
		{
			name:       "params null, secrets populated -> secrets only",
			params:     types.MapNull(types.StringType),
			secrets:    strMap(t, map[string]string{"api_key": "shh"}),
			wantErr:    false,
			wantResult: map[string]any{"api_key": "shh"},
		},
		{
			name:       "params populated, secrets null -> params only",
			params:     strMap(t, map[string]string{"region": "us-east-2"}),
			secrets:    types.MapNull(types.StringType),
			wantErr:    false,
			wantResult: map[string]any{"region": "us-east-2"},
		},
		{
			name:       "both empty non-null maps -> empty",
			params:     strMap(t, map[string]string{}),
			secrets:    strMap(t, map[string]string{}),
			wantErr:    false,
			wantResult: map[string]any{},
		},
		{
			name:    "collision -> error, nil result",
			params:  strMap(t, map[string]string{"client_id": "1"}),
			secrets: strMap(t, map[string]string{"client_id": "2"}),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, diags := mergeAuthCreds(ctx, tc.params, tc.secrets)
			if tc.wantErr {
				if !diags.HasError() {
					t.Fatalf("expected error, got out=%v", out)
				}
				if out != nil {
					t.Fatalf("expected nil result on error, got %v", out)
				}
				return
			}
			if diags.HasError() {
				t.Fatalf("unexpected diags: %v", diags)
			}
			if len(out) != len(tc.wantResult) {
				t.Fatalf("out = %v, want %v", out, tc.wantResult)
			}
			for k, v := range tc.wantResult {
				if out[k] != v {
					t.Fatalf("out[%q] = %v, want %v", k, out[k], v)
				}
			}
		})
	}
}

// --- authChanged (task refers to this invariant as "authNeedsResend") -----
//
// Security-relevant invariant: a PUT .../auth resend is triggered only by
// method, params, or secrets_wo_version. Server-owned computed fields
// (status/credentials_fingerprint/updated_at) and the write-only secret
// values themselves must never be part of the comparison -- secret values
// are masked/absent in state and therefore cannot be diffed.

func TestAuthChangedIgnoresComputedAndSecretFields(t *testing.T) {
	base := func() *authModel {
		return &authModel{
			Method:                 types.StringValue("iam-role-direct"),
			Params:                 types.MapNull(types.StringType),
			SecretsWO:              types.MapNull(types.StringType),
			SecretsWOVersion:       types.Int64Value(1),
			Status:                 types.StringValue("AUTH_STATUS_CONNECTED"),
			CredentialsFingerprint: types.StringValue("fp-old"),
			UpdatedAt:              types.StringValue("2026-01-01T00:00:00Z"),
		}
	}

	tests := []struct {
		name    string
		mutate  func(*authModel)
		wantChg bool
	}{
		{
			name:    "identical blocks -> no resend",
			mutate:  func(a *authModel) {},
			wantChg: false,
		},
		{
			name: "status differs only -> no resend",
			mutate: func(a *authModel) {
				a.Status = types.StringValue("AUTH_STATUS_ERROR")
			},
			wantChg: false,
		},
		{
			name: "credentials_fingerprint differs only -> no resend",
			mutate: func(a *authModel) {
				a.CredentialsFingerprint = types.StringValue("fp-new")
			},
			wantChg: false,
		},
		{
			name: "updated_at differs only -> no resend",
			mutate: func(a *authModel) {
				a.UpdatedAt = types.StringValue("2026-06-01T00:00:00Z")
			},
			wantChg: false,
		},
		{
			name: "all computed fields differ -> still no resend",
			mutate: func(a *authModel) {
				a.Status = types.StringValue("AUTH_STATUS_ERROR")
				a.CredentialsFingerprint = types.StringValue("fp-new")
				a.UpdatedAt = types.StringValue("2026-06-01T00:00:00Z")
			},
			wantChg: false,
		},
		{
			name: "secrets_wo value differs -> not a trigger (write-only, never diffed)",
			mutate: func(a *authModel) {
				a.SecretsWO = strMap(t, map[string]string{"api_key": "rotated"})
			},
			wantChg: false,
		},
		{
			name: "secrets_wo_version bump -> resend",
			mutate: func(a *authModel) {
				a.SecretsWOVersion = types.Int64Value(2)
			},
			wantChg: true,
		},
		{
			name: "method differs -> resend",
			mutate: func(a *authModel) {
				a.Method = types.StringValue("iam-role-s3-inventory")
			},
			wantChg: true,
		},
		{
			name: "params differ -> resend",
			mutate: func(a *authModel) {
				a.Params = strMap(t, map[string]string{"region": "us-west-2"})
			},
			wantChg: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := base()
			tc.mutate(plan)
			if got := authChanged(plan, base()); got != tc.wantChg {
				t.Fatalf("authChanged = %v, want %v", got, tc.wantChg)
			}
		})
	}
}

// --- refreshAuthComputed ----------------------------------------------------
//
// Security-relevant invariant: refreshAuthComputed only ever writes the
// server-owned computed fields. It must NEVER populate params or secrets_wo
// from a server read (a masked/absent server value becoming state would leak
// or fabricate a secret).

func TestRefreshAuthComputedNeverTouchesSecretsOrParams(t *testing.T) {
	priorParams := strMap(t, map[string]string{"region": "us-east-2"})
	priorSecrets := strMap(t, map[string]string{"api_key": "should-never-be-overwritten"})

	tests := []struct {
		name       string
		connection map[string]any
		wantStatus string // "" means null
		wantFp     string
		wantHasUAt bool
	}{
		{
			name: "full auth doc -> all computed fields set",
			connection: map[string]any{"auth": map[string]any{
				"status":                  "AUTH_STATUS_CONNECTED",
				"credentials_fingerprint": "abc123",
				"updated_at":              float64(1786400000),
			}},
			wantStatus: "AUTH_STATUS_CONNECTED",
			wantFp:     "abc123",
			wantHasUAt: true,
		},
		{
			name:       "no auth key at all -> all computed fields null",
			connection: map[string]any{},
			wantStatus: "",
			wantFp:     "",
			wantHasUAt: false,
		},
		{
			name:       "auth present but fields missing -> null, not zero values",
			connection: map[string]any{"auth": map[string]any{}},
			wantStatus: "",
			wantFp:     "",
			wantHasUAt: false,
		},
		{
			name: "wrong-typed fields (e.g. updated_at as string) -> treated as absent",
			connection: map[string]any{"auth": map[string]any{
				"status":                  "AUTH_STATUS_CONNECTED",
				"credentials_fingerprint": "abc123",
				"updated_at":              "not-a-number",
			}},
			wantStatus: "AUTH_STATUS_CONNECTED",
			wantFp:     "abc123",
			wantHasUAt: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &resourceModel{Auth: &authModel{
				Params:    priorParams,
				SecretsWO: priorSecrets,
			}}
			m.refreshAuthComputed(&client.ConnectionDetail{Connection: tc.connection})

			if tc.wantStatus == "" {
				if !m.Auth.Status.IsNull() {
					t.Fatalf("status = %v, want null", m.Auth.Status)
				}
			} else if m.Auth.Status.ValueString() != tc.wantStatus {
				t.Fatalf("status = %v, want %v", m.Auth.Status, tc.wantStatus)
			}

			if tc.wantFp == "" {
				if !m.Auth.CredentialsFingerprint.IsNull() {
					t.Fatalf("fingerprint = %v, want null", m.Auth.CredentialsFingerprint)
				}
			} else if m.Auth.CredentialsFingerprint.ValueString() != tc.wantFp {
				t.Fatalf("fingerprint = %v, want %v", m.Auth.CredentialsFingerprint, tc.wantFp)
			}

			if tc.wantHasUAt == m.Auth.UpdatedAt.IsNull() {
				t.Fatalf("updated_at null = %v, want hasValue=%v", m.Auth.UpdatedAt.IsNull(), tc.wantHasUAt)
			}

			// Invariant under test: params/secrets_wo are byte-for-byte
			// untouched by a computed-field refresh, regardless of what the
			// server auth doc contains.
			if !m.Auth.Params.Equal(priorParams) {
				t.Fatalf("params mutated by refreshAuthComputed: got %v, want untouched %v", m.Auth.Params, priorParams)
			}
			if !m.Auth.SecretsWO.Equal(priorSecrets) {
				t.Fatalf("secrets_wo mutated by refreshAuthComputed: got %v, want untouched %v", m.Auth.SecretsWO, priorSecrets)
			}
		})
	}
}

// --- fieldsToParams ---------------------------------------------------------

func TestFieldsToParams(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		in   types.Map
		want []map[string]any // order-independent
	}{
		{
			name: "null map -> nil, no diags",
			in:   types.MapNull(types.StringType),
			want: nil,
		},
		{
			name: "unknown map -> nil, no diags",
			in:   types.MapUnknown(types.StringType),
			want: nil,
		},
		{
			name: "empty map -> empty slice",
			in:   strMap(t, map[string]string{}),
			want: []map[string]any{},
		},
		{
			name: "single field",
			in:   strMap(t, map[string]string{"sampling": "10"}),
			want: []map[string]any{{"key": "sampling", "value": "10"}},
		},
		{
			name: "multiple fields -> one customFields entry per key",
			in:   strMap(t, map[string]string{"sampling": "10", "region": "us-east-2"}),
			want: []map[string]any{
				{"key": "sampling", "value": "10"},
				{"key": "region", "value": "us-east-2"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, diags := fieldsToParams(ctx, tc.in)
			if diags.HasError() {
				t.Fatalf("unexpected diags: %v", diags)
			}
			if len(out) != len(tc.want) {
				t.Fatalf("len(out) = %d, want %d (out=%v)", len(out), len(tc.want), out)
			}
			gotSorted := sortedKeyValuePairs(out)
			wantSorted := sortedKeyValuePairs(tc.want)
			for i := range wantSorted {
				if gotSorted[i] != wantSorted[i] {
					t.Fatalf("out = %v, want %v", out, tc.want)
				}
			}
		})
	}
}

func sortedKeyValuePairs(entries []map[string]any) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e["key"].(string)+"="+e["value"].(string))
	}
	sort.Strings(out)
	return out
}

// --- rfc3339ToEpoch / toScalarUpdate ----------------------------------------

func TestToScalarUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("all null model -> empty request, no diags", func(t *testing.T) {
		m := &resourceModel{
			Name:                types.StringNull(),
			RefreshFrequency:    types.Int64Null(),
			ScanFrom:            types.StringNull(),
			DataStorageLocation: nil,
			BusinessNodeIDs:     types.SetNull(types.StringType),
			CredentialsExpireAt: types.StringNull(),
			SupportSecretAccess: types.BoolNull(),
		}
		req, diags := m.toScalarUpdate(ctx)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if req.ConnectionName != nil || req.RefreshFrequency != nil || req.StartScanFrom != nil ||
			req.DataStorageLocation != nil || req.BusinessNodeIDs != nil ||
			req.CredentialsExpireAt != nil || req.RelyanceSecretAccess != nil {
			t.Fatalf("expected all-nil request, got %+v", req)
		}
	})

	t.Run("all unknown model -> empty request, no diags", func(t *testing.T) {
		m := &resourceModel{
			Name:                types.StringUnknown(),
			RefreshFrequency:    types.Int64Unknown(),
			ScanFrom:            types.StringUnknown(),
			DataStorageLocation: &dataStorageLocationModel{Region: types.StringUnknown(), Country: types.StringUnknown(), State: types.StringUnknown()},
			BusinessNodeIDs:     types.SetUnknown(types.StringType),
			CredentialsExpireAt: types.StringUnknown(),
			SupportSecretAccess: types.BoolUnknown(),
		}
		req, diags := m.toScalarUpdate(ctx)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if req.ConnectionName != nil || req.RelyanceSecretAccess != nil {
			t.Fatalf("expected nil fields for unknown attrs, got %+v", req)
		}
	})

	t.Run("fully populated model -> every field forwarded", func(t *testing.T) {
		m := &resourceModel{
			Name:                types.StringValue("my-conn"),
			RefreshFrequency:    types.Int64Value(3600),
			ScanFrom:            types.StringValue("2026-08-01T00:00:00Z"),
			DataStorageLocation: &dataStorageLocationModel{Region: types.StringValue("us")},
			BusinessNodeIDs:     types.SetValueMust(types.StringType, strAttrValues("a", "b")),
			CredentialsExpireAt: types.StringValue("2027-01-01T00:00:00Z"),
			SupportSecretAccess: types.BoolValue(true),
		}
		req, diags := m.toScalarUpdate(ctx)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if req.ConnectionName == nil || *req.ConnectionName != "my-conn" {
			t.Fatalf("ConnectionName = %v", req.ConnectionName)
		}
		if req.RefreshFrequency == nil || *req.RefreshFrequency != 3600 {
			t.Fatalf("RefreshFrequency = %v", req.RefreshFrequency)
		}
		wantEpoch := float64(1785542400) // 2026-08-01T00:00:00Z
		if req.StartScanFrom == nil || *req.StartScanFrom != wantEpoch {
			t.Fatalf("StartScanFrom = %v, want %v", req.StartScanFrom, wantEpoch)
		}
		if req.DataStorageLocation["region"] != "us" {
			t.Fatalf("DataStorageLocation = %v", req.DataStorageLocation)
		}
		if len(req.BusinessNodeIDs) != 2 {
			t.Fatalf("BusinessNodeIDs = %v", req.BusinessNodeIDs)
		}
		if req.CredentialsExpireAt == nil {
			t.Fatal("CredentialsExpireAt should be set")
		}
		if req.RelyanceSecretAccess == nil || !*req.RelyanceSecretAccess {
			t.Fatalf("RelyanceSecretAccess = %v", req.RelyanceSecretAccess)
		}
	})

	t.Run("invalid scan_from timestamp -> attribute error, no partial success", func(t *testing.T) {
		m := &resourceModel{
			Name:                types.StringNull(),
			RefreshFrequency:    types.Int64Null(),
			ScanFrom:            types.StringValue("not-a-timestamp"),
			DataStorageLocation: nil,
			BusinessNodeIDs:     types.SetNull(types.StringType),
			CredentialsExpireAt: types.StringNull(),
			SupportSecretAccess: types.BoolNull(),
		}
		_, diags := m.toScalarUpdate(ctx)
		if !diags.HasError() {
			t.Fatal("expected diagnostic error for invalid scan_from timestamp")
		}
	})

	t.Run("invalid credentials_expire_at timestamp -> attribute error", func(t *testing.T) {
		m := &resourceModel{
			Name:                types.StringNull(),
			RefreshFrequency:    types.Int64Null(),
			ScanFrom:            types.StringNull(),
			DataStorageLocation: nil,
			BusinessNodeIDs:     types.SetNull(types.StringType),
			CredentialsExpireAt: types.StringValue("garbage"),
			SupportSecretAccess: types.BoolNull(),
		}
		_, diags := m.toScalarUpdate(ctx)
		if !diags.HasError() {
			t.Fatal("expected diagnostic error for invalid credentials_expire_at timestamp")
		}
	})
}

func strAttrValues(vals ...string) []attr.Value {
	out := make([]attr.Value, 0, len(vals))
	for _, v := range vals {
		out = append(out, types.StringValue(v))
	}
	return out
}

// --- refreshFromAPI ----------------------------------------------------------
//
// refreshFromAPI's core invariant: Optional-only attributes the practitioner
// never set (null in state going in) MUST stay null even when the server
// reports a value -- a server-side default is not config drift. Fields the
// practitioner did set get overlaid with server truth.

func TestRefreshFromAPIRespectsPractitionerOwnership(t *testing.T) {
	ctx := context.Background()

	t.Run("name always overlaid (server-owned)", func(t *testing.T) {
		m := &resourceModel{Name: types.StringValue("old-name")}
		diags := m.refreshFromAPI(ctx, &client.ConnectionDetail{Connection: map[string]any{
			"connection_name": "server-name",
		}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if m.Name.ValueString() != "server-name" {
			t.Fatalf("Name = %v, want server-name", m.Name)
		}
	})

	t.Run("integration_type must always be known: value when present", func(t *testing.T) {
		m := &resourceModel{Name: types.StringNull()}
		diags := m.refreshFromAPI(ctx, &client.ConnectionDetail{Connection: map[string]any{
			"integrationType": "aws_s3",
		}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if m.IntegrationType.ValueString() != "aws_s3" {
			t.Fatalf("IntegrationType = %v, want aws_s3", m.IntegrationType)
		}
	})

	t.Run("integration_type must always be known: null when absent, never unknown", func(t *testing.T) {
		m := &resourceModel{}
		diags := m.refreshFromAPI(ctx, &client.ConnectionDetail{Connection: map[string]any{}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if !m.IntegrationType.IsNull() {
			t.Fatalf("IntegrationType = %v, want null", m.IntegrationType)
		}
		if m.IntegrationType.IsUnknown() {
			t.Fatal("IntegrationType must never be left unknown after apply")
		}
	})

	t.Run("optional fields the practitioner never set stay null despite server value", func(t *testing.T) {
		m := &resourceModel{
			RefreshFrequency:    types.Int64Null(),
			ScanFrom:            types.StringNull(),
			DataStorageLocation: nil,
			BusinessNodeIDs:     types.SetNull(types.StringType),
			SupportSecretAccess: types.BoolNull(),
		}
		diags := m.refreshFromAPI(ctx, &client.ConnectionDetail{Connection: map[string]any{
			"refreshFrequency":      float64(900),
			"startScanFrom":         float64(1785542400),
			"data_storage_location": map[string]any{"region": "eu"},
			"business_node_ids":     []any{"n1"},
			"relyanceSecretAccess":  true,
		}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if !m.RefreshFrequency.IsNull() {
			t.Fatalf("RefreshFrequency = %v, want null (server default is not drift)", m.RefreshFrequency)
		}
		if !m.ScanFrom.IsNull() {
			t.Fatalf("ScanFrom = %v, want null", m.ScanFrom)
		}
		if m.DataStorageLocation != nil {
			t.Fatalf("DataStorageLocation = %v, want null", m.DataStorageLocation)
		}
		if !m.BusinessNodeIDs.IsNull() {
			t.Fatalf("BusinessNodeIDs = %v, want null", m.BusinessNodeIDs)
		}
		if !m.SupportSecretAccess.IsNull() {
			t.Fatalf("SupportSecretAccess = %v, want null", m.SupportSecretAccess)
		}
	})

	t.Run("fields the practitioner set are overlaid with server truth", func(t *testing.T) {
		m := &resourceModel{
			RefreshFrequency:    types.Int64Value(60), // practitioner had set some prior value
			ScanFrom:            types.StringValue("2020-01-01T00:00:00Z"),
			DataStorageLocation: &dataStorageLocationModel{Region: types.StringValue("us")},
			BusinessNodeIDs:     types.SetValueMust(types.StringType, strAttrValues("old")),
			SupportSecretAccess: types.BoolValue(false),
		}
		diags := m.refreshFromAPI(ctx, &client.ConnectionDetail{Connection: map[string]any{
			"refreshFrequency":      float64(900),
			"startScanFrom":         float64(1785542400),
			"data_storage_location": map[string]any{"region": "eu"},
			"business_node_ids":     []any{"n1", "n2"},
			"relyanceSecretAccess":  true,
		}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if m.RefreshFrequency.ValueInt64() != 900 {
			t.Fatalf("RefreshFrequency = %v, want 900", m.RefreshFrequency)
		}
		if m.ScanFrom.ValueString() != "2026-08-01T00:00:00Z" {
			t.Fatalf("ScanFrom = %v, want 2026-08-01T00:00:00Z", m.ScanFrom)
		}
		if m.DataStorageLocation == nil || m.DataStorageLocation.Region.ValueString() != "eu" {
			t.Fatalf("DataStorageLocation = %v, want region=eu", m.DataStorageLocation)
		}
		if m.SupportSecretAccess.ValueBool() != true {
			t.Fatal("SupportSecretAccess should be overlaid to true")
		}
		var ids []string
		diags.Append(m.BusinessNodeIDs.ElementsAs(ctx, &ids, false)...)
		if len(ids) != 2 {
			t.Fatalf("BusinessNodeIDs = %v, want 2 entries", ids)
		}
	})
}

// --- scansFromMap / refreshScans --------------------------------------------

func TestScansFromMap(t *testing.T) {
	ctx := context.Background()

	t.Run("null map -> nil, no diags", func(t *testing.T) {
		out, diags := scansFromMap(ctx, types.MapNull(scanObjectType()))
		if diags.HasError() || out != nil {
			t.Fatalf("out=%v diags=%v", out, diags)
		}
	})

	t.Run("unknown map -> nil, no diags", func(t *testing.T) {
		out, diags := scansFromMap(ctx, types.MapUnknown(scanObjectType()))
		if diags.HasError() || out != nil {
			t.Fatalf("out=%v diags=%v", out, diags)
		}
	})

	t.Run("populated map decodes slug -> scanModel", func(t *testing.T) {
		in := scanMap(t, map[string]scanModel{
			"data-inspection": {Enabled: types.BoolValue(true), Fields: types.MapNull(types.StringType)},
		})
		out, diags := scansFromMap(ctx, in)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		sc, ok := out["data-inspection"]
		if !ok || !sc.Enabled.ValueBool() {
			t.Fatalf("out = %v", out)
		}
	})
}

func TestRefreshScans(t *testing.T) {
	ctx := context.Background()

	t.Run("null scans -> no-op", func(t *testing.T) {
		m := &resourceModel{Scans: types.MapNull(scanObjectType())}
		diags := m.refreshScans(ctx, &client.ConnectionDetail{Connection: map[string]any{}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if !m.Scans.IsNull() {
			t.Fatalf("Scans = %v, want still null", m.Scans)
		}
	})

	t.Run("desired slug absent from server kinds -> kept as-is, no invention", func(t *testing.T) {
		fields := strMap(t, map[string]string{"sampling": "5"})
		m := &resourceModel{Scans: scanMap(t, map[string]scanModel{
			"data-inspection": {Enabled: types.BoolValue(true), Fields: fields},
		})}
		diags := m.refreshScans(ctx, &client.ConnectionDetail{Connection: map[string]any{
			"kinds": map[string]any{},
		}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		out, d := scansFromMap(ctx, m.Scans)
		if d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		if len(out) != 1 {
			t.Fatalf("refreshScans must not invent or drop entries: got %v", out)
		}
		sc := out["data-inspection"]
		if !sc.Enabled.ValueBool() || !sc.Fields.Equal(fields) {
			t.Fatalf("scan entry mutated when server had no matching kind: %v", sc)
		}
	})

	t.Run("server isSelected overrides Enabled but Fields stay the practitioner's", func(t *testing.T) {
		fields := strMap(t, map[string]string{"sampling": "5"})
		m := &resourceModel{Scans: scanMap(t, map[string]scanModel{
			"data-inspection": {Enabled: types.BoolValue(true), Fields: fields},
		})}
		diags := m.refreshScans(ctx, &client.ConnectionDetail{Connection: map[string]any{
			"kinds": map[string]any{
				"INTEGRATION_KIND_DATA_INSPECTION": map[string]any{"isSelected": false},
			},
		}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		out, _ := scansFromMap(ctx, m.Scans)
		sc := out["data-inspection"]
		if sc.Enabled.ValueBool() {
			t.Fatal("Enabled should be overridden to false from server isSelected")
		}
		if !sc.Fields.Equal(fields) {
			t.Fatalf("Fields should be untouched: got %v, want %v", sc.Fields, fields)
		}
	})

	t.Run("server-only kind not in desired is ignored, not surfaced", func(t *testing.T) {
		m := &resourceModel{Scans: scanMap(t, map[string]scanModel{
			"data-inspection": {Enabled: types.BoolValue(true), Fields: types.MapNull(types.StringType)},
		})}
		diags := m.refreshScans(ctx, &client.ConnectionDetail{Connection: map[string]any{
			"kinds": map[string]any{
				"INTEGRATION_KIND_DATA_INSPECTION":  map[string]any{"isSelected": true},
				"INTEGRATION_KIND_ASSETS_DISCOVERY": map[string]any{"isSelected": true},
			},
		}})
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		out, _ := scansFromMap(ctx, m.Scans)
		if len(out) != 1 {
			t.Fatalf("refreshScans surfaced a kind the practitioner never declared: %v", out)
		}
	})
}
