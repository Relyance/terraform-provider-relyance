// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connection

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/relyance/terraform-provider-relyance/internal/client"
)

func TestApplyScansSavesPendingFieldsWhenEnabled(t *testing.T) {
	fields := strMap(t, map[string]string{"store_samples": "false"})
	state := scanMap(t, map[string]scanModel{"data-inspection": {Enabled: types.BoolValue(false), Fields: fields}})
	plan := scanMap(t, map[string]scanModel{"data-inspection": {Enabled: types.BoolValue(true), Fields: fields}})
	svc := &fakeService{}
	if diags := applyScans(context.Background(), svc, "aws_rds", "test", plan, state); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(svc.calls) != 2 || !strings.HasPrefix(svc.calls[0], "savekind ") {
		t.Fatalf("enabling must save pending fields before selecting, got %v", svc.calls)
	}
}

func TestRefreshScansReadsManagedFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields any
		want   map[string]string
	}{
		{"changed string checkbox", []any{map[string]any{"key": "store_samples", "value": "true"}}, map[string]string{"store_samples": "true"}},
		{"changed boolean checkbox", []any{map[string]any{"key": "store_samples", "value": true}}, map[string]string{"store_samples": "true"}},
		{"deleted managed field", []any{}, map[string]string{}},
		{"missing customFields", nil, map[string]string{}},
		{"server defaults stay unmanaged", []any{map[string]any{"key": "store_samples", "value": "false"}, map[string]any{"key": "sample_retention_days", "value": "30"}}, map[string]string{"store_samples": "false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &resourceModel{Scans: scanMap(t, map[string]scanModel{
				"data-inspection": {Enabled: types.BoolValue(true), Fields: strMap(t, map[string]string{"store_samples": "false"})},
			})}
			kind := map[string]any{"isSelected": true}
			if tc.fields != nil {
				kind["customFields"] = tc.fields
			}
			d := &client.ConnectionDetail{Connection: map[string]any{"kinds": map[string]any{"INTEGRATION_KIND_DATA_INSPECTION": kind}}}
			if diags := m.refreshScans(context.Background(), d); diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			out, diags := scansFromMap(context.Background(), m.Scans)
			if diags.HasError() || !out["data-inspection"].Fields.Equal(strMap(t, tc.want)) {
				t.Fatalf("managed fields = %v, want %v; diagnostics=%v", out["data-inspection"].Fields, tc.want, diags)
			}
		})
	}
}

func TestRefreshScansReadsDisabledFields(t *testing.T) {
	fields := strMap(t, map[string]string{"store_samples": "false"})
	m := &resourceModel{Scans: scanMap(t, map[string]scanModel{
		"data-inspection": {Enabled: types.BoolValue(false), Fields: fields},
	})}
	d := &client.ConnectionDetail{Connection: map[string]any{"kinds": map[string]any{
		"INTEGRATION_KIND_DATA_INSPECTION": map[string]any{"isSelected": false},
	}}}
	if diags := m.refreshScans(context.Background(), d); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	out, _ := scansFromMap(context.Background(), m.Scans)
	if !out["data-inspection"].Fields.Equal(strMap(t, map[string]string{})) {
		t.Fatalf("missing disabled fields must be planned back before activation: %v", out)
	}
}

func TestRefreshScansRejectsMissingManagedKind(t *testing.T) {
	ctx := context.Background()
	for _, kinds := range []any{nil, map[string]any{}} {
		m := &resourceModel{Scans: scanMap(t, map[string]scanModel{
			"data-inspection": {Enabled: types.BoolValue(true), Fields: strMap(t, map[string]string{"store_samples": "false"})},
		})}
		prior := m.Scans
		d := &client.ConnectionDetail{Connection: map[string]any{"kinds": kinds}}
		diags := m.refreshScans(ctx, d)
		if !diags.HasError() {
			t.Fatal("missing managed kind must not report prior scan settings as verified")
		}
		if !m.Scans.Equal(prior) {
			t.Fatal("failed refresh must preserve state until the missing server kind is resolved")
		}
	}
}
