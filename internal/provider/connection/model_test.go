// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connection

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

func strMap(t *testing.T, m map[string]string) types.Map {
	t.Helper()
	out, diags := types.MapValueFrom(context.Background(), types.StringType, m)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return out
}

func TestMergeAuthCreds(t *testing.T) {
	ctx := context.Background()

	t.Run("union", func(t *testing.T) {
		out, diags := mergeAuthCreds(ctx, strMap(t, map[string]string{"a": "1"}), strMap(t, map[string]string{"b": "2"}))
		if diags.HasError() {
			t.Fatal(diags)
		}
		if out["a"] != "1" || out["b"] != "2" || len(out) != 2 {
			t.Fatalf("out = %v", out)
		}
	})

	t.Run("collision errors", func(t *testing.T) {
		_, diags := mergeAuthCreds(ctx, strMap(t, map[string]string{"a": "1"}), strMap(t, map[string]string{"a": "2"}))
		if !diags.HasError() {
			t.Fatal("expected collision error")
		}
	})

	t.Run("null maps are empty", func(t *testing.T) {
		out, diags := mergeAuthCreds(ctx, types.MapNull(types.StringType), types.MapNull(types.StringType))
		if diags.HasError() || len(out) != 0 {
			t.Fatalf("out=%v diags=%v", out, diags)
		}
	})
}

func TestAuthChanged(t *testing.T) {
	base := func() *authModel {
		return &authModel{
			Method:           types.StringValue("iam-role-direct"),
			Params:           types.MapNull(types.StringType),
			SecretsWOVersion: types.Int64Value(1),
		}
	}

	if authChanged(nil, base()) {
		t.Fatal("nil plan never triggers")
	}
	if !authChanged(base(), nil) {
		t.Fatal("new auth block triggers")
	}
	if authChanged(base(), base()) {
		t.Fatal("identical blocks must not trigger")
	}

	bumped := base()
	bumped.SecretsWOVersion = types.Int64Value(2)
	if !authChanged(bumped, base()) {
		t.Fatal("version bump triggers resend")
	}

	method := base()
	method.Method = types.StringValue("iam-role-s3-inventory")
	if !authChanged(method, base()) {
		t.Fatal("method change triggers resend")
	}

	params := base()
	params.Params = strMap(t, map[string]string{"region": "us-east-2"})
	if !authChanged(params, base()) {
		t.Fatal("params change triggers resend")
	}
}

func TestRefreshAuthComputed(t *testing.T) {
	m := &resourceModel{Auth: &authModel{}}
	m.refreshAuthComputed(&client.ConnectionDetail{Connection: map[string]any{
		"auth": map[string]any{
			"status":                  "AUTH_STATUS_CONNECTED",
			"credentials_fingerprint": "abc123",
			"updated_at":              float64(1786400000),
		},
	}})
	if m.Auth.Status.ValueString() != "AUTH_STATUS_CONNECTED" {
		t.Fatalf("status = %v", m.Auth.Status)
	}
	if m.Auth.CredentialsFingerprint.ValueString() != "abc123" {
		t.Fatalf("fingerprint = %v", m.Auth.CredentialsFingerprint)
	}
	if m.Auth.UpdatedAt.IsNull() {
		t.Fatal("updated_at should be set")
	}

	// No auth on the server → computed go null, params/secrets untouched.
	m2 := &resourceModel{Auth: &authModel{Params: strMap(t, map[string]string{"a": "1"})}}
	m2.refreshAuthComputed(&client.ConnectionDetail{Connection: map[string]any{}})
	if !m2.Auth.Status.IsNull() {
		t.Fatal("status should be null")
	}
	if m2.Auth.Params.IsNull() {
		t.Fatal("params must never be touched by refresh")
	}
}

func TestTimestampRoundTrip(t *testing.T) {
	if got := epochToRFC3339(1786400000); got != "2026-08-10T22:13:20Z" {
		t.Fatalf("epochToRFC3339 = %q", got)
	}
}

func scanMap(t *testing.T, entries map[string]scanModel) types.Map {
	t.Helper()
	m, diags := types.MapValueFrom(context.Background(), scanObjectType(), entries)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return m
}

type recordSvc struct {
	Service
	calls []string
}

func (r *recordSvc) SelectKind(_ context.Context, _, _, kind string, selected bool) error {
	r.calls = append(r.calls, fmt.Sprintf("select %s=%v", kind, selected))
	return nil
}
func (r *recordSvc) SaveKind(_ context.Context, _, _, kind string, params []map[string]any) error {
	r.calls = append(r.calls, fmt.Sprintf("save %s(%d)", kind, len(params)))
	return nil
}

func TestApplyScansDiffing(t *testing.T) {
	ctx := context.Background()
	on := func(fields map[string]string) scanModel {
		f := types.MapNull(types.StringType)
		if fields != nil {
			f = strMap(t, fields)
		}
		return scanModel{Enabled: types.BoolValue(true), Fields: f}
	}

	// New scan with fields → select + save.
	svc := &recordSvc{}
	plan := scanMap(t, map[string]scanModel{"data-inspection": on(map[string]string{"sampling": "10"})})
	applyScans(ctx, svc, "aws_s3", "1", plan, types.MapNull(scanObjectType()))
	if len(svc.calls) != 2 || svc.calls[0] != "select data-inspection=true" || svc.calls[1] != "save data-inspection(1)" {
		t.Fatalf("new-scan calls = %v", svc.calls)
	}

	// Unchanged → no calls.
	svc = &recordSvc{}
	applyScans(ctx, svc, "aws_s3", "1", plan, plan)
	if len(svc.calls) != 0 {
		t.Fatalf("unchanged calls = %v", svc.calls)
	}

	// Removed from config → deselect.
	svc = &recordSvc{}
	applyScans(ctx, svc, "aws_s3", "1", types.MapNull(scanObjectType()), plan)
	if len(svc.calls) != 1 || svc.calls[0] != "select data-inspection=false" {
		t.Fatalf("removed calls = %v", svc.calls)
	}
}

func TestKindSlug(t *testing.T) {
	cases := map[string]string{
		"INTEGRATION_KIND_DATA_INSPECTION":  "data-inspection",
		"INTEGRATION_KIND_ASSETS_DISCOVERY": "assets-discovery",
		// DSRS is the one kind whose slug is not a mechanical derivation.
		"INTEGRATION_KIND_DSRS":            "data-subject-requests",
		"INTEGRATION_KIND_UNKNOWN_FUTURE0": "unknown-future0", // fallback derivation
	}
	for enum, want := range cases {
		if got := kindSlug(enum); got != want {
			t.Fatalf("kindSlug(%s) = %s, want %s", enum, got, want)
		}
	}
}
