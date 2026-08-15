// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connection

import (
	"context"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

// Service is the only layer that talks HTTP. resource.go stays pure
// Terraform-framework plumbing; tests substitute a fake Service.
type Service interface {
	Create(ctx context.Context, vendorKey string, req client.CreateConnectionRequest) (string, error)
	Get(ctx context.Context, vendorKey, id string) (*client.ConnectionDetail, error)
	UpdateScalars(ctx context.Context, vendorKey, id string, req client.ScalarUpdateRequest) error
	Delete(ctx context.Context, vendorKey, id string) error
	GetVendor(ctx context.Context, vendorKey string) (*client.Vendor, error)
	SaveAuth(ctx context.Context, vendorKey, id string, req client.AuthSaveRequest) error
	ValidateAuth(ctx context.Context, vendorKey, id string, req client.AuthSaveRequest) (*client.ValidateResult, error)
	SelectKind(ctx context.Context, vendorKey, id, kind string, selected bool) error
	SaveKind(ctx context.Context, vendorKey, id, kind string, params []map[string]any) error
	Connect(ctx context.Context, vendorKey, id string) (*client.ConnectStatus, error)
}

type service struct{ c *client.Client }

// NewService wraps the shared API client.
func NewService(c *client.Client) Service { return &service{c: c} }

func (s *service) Create(ctx context.Context, vendorKey string, req client.CreateConnectionRequest) (string, error) {
	return s.c.CreateConnection(ctx, vendorKey, req)
}

func (s *service) Get(ctx context.Context, vendorKey, id string) (*client.ConnectionDetail, error) {
	return s.c.GetConnection(ctx, vendorKey, id)
}

func (s *service) UpdateScalars(ctx context.Context, vendorKey, id string, req client.ScalarUpdateRequest) error {
	return s.c.UpdateConnectionScalars(ctx, vendorKey, id, req)
}

func (s *service) Delete(ctx context.Context, vendorKey, id string) error {
	return s.c.DeleteConnection(ctx, vendorKey, id)
}

func (s *service) GetVendor(ctx context.Context, vendorKey string) (*client.Vendor, error) {
	return s.c.GetVendor(ctx, vendorKey)
}

func (s *service) SaveAuth(ctx context.Context, vendorKey, id string, req client.AuthSaveRequest) error {
	return s.c.SaveAuth(ctx, vendorKey, id, req)
}

func (s *service) ValidateAuth(ctx context.Context, vendorKey, id string, req client.AuthSaveRequest) (*client.ValidateResult, error) {
	return s.c.ValidateAuth(ctx, vendorKey, id, req)
}

func (s *service) SelectKind(ctx context.Context, vendorKey, id, kind string, selected bool) error {
	return s.c.SelectKind(ctx, vendorKey, id, kind, selected)
}

func (s *service) SaveKind(ctx context.Context, vendorKey, id, kind string, params []map[string]any) error {
	return s.c.SaveKind(ctx, vendorKey, id, kind, params)
}

func (s *service) Connect(ctx context.Context, vendorKey, id string) (*client.ConnectStatus, error) {
	return s.c.Connect(ctx, vendorKey, id)
}
