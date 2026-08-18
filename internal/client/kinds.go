// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"

	"github.com/relyance/terraform-provider-relyance/internal/apiclient"
)

// KindSaveRequest mirrors KindSaveRequest: parameters is the customFields list.
type KindSaveRequest struct {
	Parameters []map[string]any `json:"parameters"`
}

// KindSelectRequest mirrors KindSelectRequest.
type KindSelectRequest struct {
	Selected bool `json:"selected"`
}

// SelectKind selects or deselects a kind (204).
func (c *Client) SelectKind(ctx context.Context, vendorKey, id, kind string, selected bool) error {
	body := apiclient.KindSelectRequest{Selected: selected}
	resp, err := c.api.TerraformAPI.SelectVendorConnectionKindV1(ctx, vendorKey, id, kind).
		KindSelectRequest(body).Execute()
	if err != nil {
		return apiErr(resp, err)
	}
	return nil
}

// SaveKind saves a selected kind's custom-field values (204).
func (c *Client) SaveKind(ctx context.Context, vendorKey, id, kind string, params []map[string]any) error {
	body := apiclient.KindSaveRequest{Parameters: params}
	resp, err := c.api.TerraformAPI.SaveVendorConnectionKindV1(ctx, vendorKey, id, kind).
		KindSaveRequest(body).Execute()
	if err != nil {
		return apiErr(resp, err)
	}
	return nil
}
