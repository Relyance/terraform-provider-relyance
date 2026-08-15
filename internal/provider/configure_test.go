// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestResolveTimeout(t *testing.T) {
	// Isolate from any ambient RELYANCE_TIMEOUT_SECONDS on the host.
	t.Setenv("RELYANCE_TIMEOUT_SECONDS", "")
	cases := []struct {
		name string
		in   types.Int64
		want time.Duration
	}{
		{"unset -> default", types.Int64Null(), defaultTimeoutSec * time.Second},
		{"explicit 30", types.Int64Value(30), 30 * time.Second},
		{"explicit 0 -> clamp to default", types.Int64Value(0), defaultTimeoutSec * time.Second},
		{"negative -> clamp to default", types.Int64Value(-5), defaultTimeoutSec * time.Second},
	}
	for _, c := range cases {
		got := resolveTimeout(providerModel{TimeoutSec: c.in})
		if got != c.want {
			t.Errorf("%s: resolveTimeout = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRequireSecureEndpoint(t *testing.T) {
	cases := []struct {
		endpoint string
		ok       bool
	}{
		{"https://beta.api.relyance.ai", true},
		{"https://api.relyance.ai", true},
		{"http://localhost:18000", true},
		{"http://127.0.0.1:8000", true},
		{"http://beta.api.relyance.ai", false}, // cleartext to a real host — reject
		{"http://evil.example.com", false},
		{"ftp://api.relyance.ai", false},
		{"://nonsense", false},
	}
	for _, c := range cases {
		err := requireSecureEndpoint(c.endpoint)
		if c.ok && err != nil {
			t.Errorf("requireSecureEndpoint(%q) = %v, want nil", c.endpoint, err)
		}
		if !c.ok && err == nil {
			t.Errorf("requireSecureEndpoint(%q) = nil, want error", c.endpoint)
		}
	}
}
