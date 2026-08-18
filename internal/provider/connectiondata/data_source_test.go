// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectiondata

import (
	"testing"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

func TestStrOrNull(t *testing.T) {
	d := &client.ConnectionDetail{Connection: map[string]any{"connection_name": "prod", "count": float64(3)}}

	if got := strOrNull(d, "connection_name"); got.ValueString() != "prod" {
		t.Fatalf("present string key = %v, want prod", got)
	}
	if got := strOrNull(d, "missing"); !got.IsNull() {
		t.Fatalf("missing key = %v, want null", got)
	}
	if got := strOrNull(d, "count"); !got.IsNull() {
		t.Fatalf("non-string value = %v, want null", got)
	}
}

func TestAnyStr(t *testing.T) {
	if got := anyStr("AUTH_STATUS_CONNECTED"); got.ValueString() != "AUTH_STATUS_CONNECTED" {
		t.Fatalf("string = %v, want AUTH_STATUS_CONNECTED", got)
	}
	if got := anyStr(nil); !got.IsNull() {
		t.Fatalf("nil = %v, want null", got)
	}
	if got := anyStr(42); !got.IsNull() {
		t.Fatalf("non-string = %v, want null", got)
	}
}
