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
	if len(svc.calls) != 2 || !strings.HasPrefix(svc.calls[1], "savekind ") {
		t.Fatalf("enabling must select then save pending fields, got %v", svc.calls)
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

func TestRefreshScansPreservesUnappliedDisabledFields(t *testing.T) {
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
	if !out["data-inspection"].Fields.Equal(fields) {
		t.Fatalf("disabled fields must stay configured until the scan is enabled: %v", out)
	}
}
