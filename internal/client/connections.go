// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"

	"github.com/relyance/terraform-provider-relyance/internal/apiclient"
)

// ListConnections returns a vendor's connections. 404 (vendor not present in
// the tenant) maps to common.ErrNotFound.
func (c *Client) ListConnections(ctx context.Context, vendorKey string) ([]ConnectionSummary, error) {
	items, resp, err := c.api.TerraformAPI.ListVendorConnectionsV1(ctx, vendorKey).Execute()
	if err != nil {
		return nil, apiErr(resp, err)
	}
	out := make([]ConnectionSummary, 0, len(items))
	for i := range items {
		raw := items[i].AdditionalProperties
		if raw == nil {
			raw = map[string]any{}
		}
		raw["id"] = items[i].Id
		out = append(out, ConnectionSummary{ID: items[i].Id, Raw: raw})
	}
	return out, nil
}

// GetConnection returns full detail (connection + hydrated masked auth/kind
// configs).
func (c *Client) GetConnection(ctx context.Context, vendorKey, id string) (*ConnectionDetail, error) {
	d, resp, err := c.api.TerraformAPI.GetVendorConnectionV1(ctx, vendorKey, id).Execute()
	if err != nil {
		return nil, apiErr(resp, err)
	}
	return &ConnectionDetail{
		Connection:  d.Connection,
		AuthConfigs: d.AuthConfigs,
		KindConfigs: d.KindConfigs,
	}, nil
}

// CreateConnection creates a connection and returns its allocated id
// (201 + Location + summary body).
func (c *Client) CreateConnection(ctx context.Context, vendorKey string, req CreateConnectionRequest) (string, error) {
	body := apiclient.CreateConnectionRequest{ConnectionName: req.ConnectionName}
	if req.IntegrationType != nil {
		body.SetIntegrationType(*req.IntegrationType)
	}
	summary, resp, err := c.api.TerraformAPI.CreateVendorConnectionV1(ctx, vendorKey).CreateConnectionRequest(body).Execute()
	if err != nil {
		return "", apiErr(resp, err)
	}
	if summary == nil || summary.Id == "" {
		return "", fmt.Errorf("create response missing connection id")
	}
	return summary.Id, nil
}

// UpdateConnectionScalars PATCHes single-field setters (204). Only fields set on
// req are sent; the rest are left untouched server-side.
func (c *Client) UpdateConnectionScalars(ctx context.Context, vendorKey, id string, req ScalarUpdateRequest) error {
	resp, err := c.api.TerraformAPI.UpdateVendorConnectionV1(ctx, vendorKey, id).
		ScalarUpdateRequest(scalarUpdateToAPI(req)).Execute()
	if err != nil {
		return apiErr(resp, err)
	}
	return nil
}

// DeleteConnection deletes a connection (204; server does best-effort secret
// cleanup first).
func (c *Client) DeleteConnection(ctx context.Context, vendorKey, id string) error {
	resp, err := c.api.TerraformAPI.DeleteVendorConnectionV1(ctx, vendorKey, id).Execute()
	if err != nil {
		return apiErr(resp, err)
	}
	return nil
}

// scalarUpdateToAPI maps the facade's optional-pointer request onto the
// generated setters. Every field is threaded through so a server-side rename or
// type change surfaces as a compile error here.
func scalarUpdateToAPI(req ScalarUpdateRequest) apiclient.ScalarUpdateRequest {
	var b apiclient.ScalarUpdateRequest
	if req.ConnectionName != nil {
		b.SetConnectionName(*req.ConnectionName)
	}
	if req.RefreshFrequency != nil {
		b.SetRefreshFrequency(*req.RefreshFrequency)
	}
	if req.StartScanFrom != nil {
		b.SetStartScanFrom(*req.StartScanFrom)
	}
	if req.DataStorageLocation != nil {
		b.SetDataStorageLocation(dataStorageLocationToAPI(req.DataStorageLocation))
	}
	if req.BusinessNodeIDs != nil {
		b.SetBusinessNodeIds(req.BusinessNodeIDs)
	}
	if req.CredentialsExpireAt != nil {
		// Server types this as an integer epoch; the provider carries the epoch
		// as a float64 (shared with startScanFrom). Whole seconds cast losslessly.
		b.SetCredentialsExpireAt(int64(*req.CredentialsExpireAt))
	}
	if req.RelyanceSecretAccess != nil {
		b.SetRelyanceSecretAccess(*req.RelyanceSecretAccess)
	}
	return b
}

// dataStorageLocationToAPI maps the facade's string map onto the typed
// DataStorageLocation object. Known keys (region/country/state) land on typed
// fields; any others ride along as additional properties so nothing is dropped.
func dataStorageLocationToAPI(m map[string]string) apiclient.DataStorageLocation {
	var d apiclient.DataStorageLocation
	var extra map[string]any
	for k := range m {
		v := m[k]
		switch k {
		case "region":
			d.SetRegion(v)
		case "country":
			d.SetCountry(v)
		case "state":
			d.SetState(v)
		default:
			if extra == nil {
				extra = map[string]any{}
			}
			extra[k] = v
		}
	}
	d.AdditionalProperties = extra
	return d
}
