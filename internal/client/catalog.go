// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"

	"github.com/relyance/terraform-provider-relyance/internal/apiclient"
)

// GetVendor returns one catalog entry (license/feature-filtered per tenant).
func (c *Client) GetVendor(ctx context.Context, vendorKey string) (*Vendor, error) {
	entry, resp, err := c.api.TerraformAPI.GetIntegrationCatalogEntryV1(ctx, vendorKey).Execute()
	if err != nil {
		return nil, apiErr(resp, err)
	}
	return vendorFromEntry(entry)
}

// ListVendors returns the tenant's full filtered catalog.
func (c *Client) ListVendors(ctx context.Context) ([]Vendor, error) {
	entries, resp, err := c.api.TerraformAPI.ListIntegrationCatalogV1(ctx).Execute()
	if err != nil {
		return nil, apiErr(resp, err)
	}
	out := make([]Vendor, 0, len(entries))
	for i := range entries {
		v, err := vendorFromEntry(&entries[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// vendorFromEntry reconstructs the full vendor map from the generated entry
// (vendorKey is typed; everything else, incl. authConfigs, is passthrough) then
// decodes the typed subset the provider needs.
func vendorFromEntry(entry *apiclient.CatalogEntrySerializer) (*Vendor, error) {
	raw := entry.AdditionalProperties
	if raw == nil {
		raw = map[string]any{}
	}
	raw["vendorKey"] = entry.VendorKey
	return vendorFromRaw(raw)
}

// vendorFromRaw decodes the typed subset and keeps the full payload in Raw.
func vendorFromRaw(raw map[string]any) (*Vendor, error) {
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var v Vendor
	if err := json.Unmarshal(buf, &v); err != nil {
		return nil, err
	}
	v.Raw = raw
	return &v, nil
}

// SecretFieldKeys returns which custom-field keys are secret for a vendor's
// auth config, per the catalog's isThisSecret flags.
func (v *Vendor) SecretFieldKeys(authKey string) []string {
	for _, cfg := range v.AuthConfigs {
		if cfg.Key != authKey {
			continue
		}
		var keys []string
		for _, f := range cfg.CustomFields {
			if f.IsThisSecret {
				keys = append(keys, f.Key)
			}
		}
		return keys
	}
	return nil
}
