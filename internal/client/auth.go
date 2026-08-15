// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"

	"github.com/relyance/terraform-provider-relyance/internal/apiclient"
)

// SaveAuth persists auth-field values (204). Secrets land in the server's
// secret store; non-secret fields in the connection document. The server
// validates before writing.
func (c *Client) SaveAuth(ctx context.Context, vendorKey, id string, req AuthSaveRequest) error {
	body := apiclient.AuthSaveRequest{AuthKey: req.AuthKey, CustomCreds: req.CustomCreds}
	resp, err := c.api.TerraformAPI.SaveVendorConnectionAuthV1(ctx, vendorKey, id).
		AuthSaveRequest(body).Execute()
	if err != nil {
		return apiErr(resp, err)
	}
	return nil
}

// ValidateAuth runs server-side validation without persisting anything —
// safe to call at plan time.
func (c *Client) ValidateAuth(ctx context.Context, vendorKey, id string, req AuthSaveRequest) (*ValidateResult, error) {
	body := apiclient.AuthValidateRequest{AuthKey: req.AuthKey, CustomCreds: req.CustomCreds}
	out, resp, err := c.api.TerraformAPI.ValidateVendorConnectionAuthV1(ctx, vendorKey, id).
		AuthValidateRequest(body).Execute()
	if err != nil {
		return nil, apiErr(resp, err)
	}
	res := &ValidateResult{
		IsValid:      out.IsValid,
		FieldResults: out.FieldResults,
	}
	if out.Error.IsSet() {
		res.Error = out.Error.Get()
	}
	return res, nil
}

// Connect triggers the async live-credential test (202 + monitor body); the
// final outcome lands in the connection's auth.status.
func (c *Client) Connect(ctx context.Context, vendorKey, id string) (*ConnectStatus, error) {
	out, resp, err := c.api.TerraformAPI.ConnectVendorConnectionV1(ctx, vendorKey, id).Execute()
	if err != nil {
		return nil, apiErr(resp, err)
	}
	status := ""
	if out.Status != nil {
		status = *out.Status
	}
	return &ConnectStatus{ID: out.Id, Status: status}, nil
}

// Disconnect clears connected state (204).
func (c *Client) Disconnect(ctx context.Context, vendorKey, id string) error {
	resp, err := c.api.TerraformAPI.DisconnectVendorConnectionV1(ctx, vendorKey, id).Execute()
	if err != nil {
		return apiErr(resp, err)
	}
	return nil
}
