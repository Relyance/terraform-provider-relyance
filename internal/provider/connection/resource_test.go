// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connection

import (
	"context"
	"fmt"
	"testing"

	"github.com/relyance/terraform-provider-relyance/internal/client"
	"github.com/relyance/terraform-provider-relyance/internal/common"
)

// fakeService records calls and serves canned responses.
type fakeService struct {
	calls     []string
	detail    *client.ConnectionDetail
	created   string
	getErr    error
	vendorErr error
}

func (f *fakeService) Create(_ context.Context, vendorKey string, req client.CreateConnectionRequest) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("create %s %s", vendorKey, req.ConnectionName))
	return f.created, nil
}

func (f *fakeService) Get(_ context.Context, vendorKey, id string) (*client.ConnectionDetail, error) {
	f.calls = append(f.calls, fmt.Sprintf("get %s/%s", vendorKey, id))
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.detail, nil
}

func (f *fakeService) UpdateScalars(_ context.Context, vendorKey, id string, _ client.ScalarUpdateRequest) error {
	f.calls = append(f.calls, fmt.Sprintf("patch %s/%s", vendorKey, id))
	return nil
}

func (f *fakeService) Delete(_ context.Context, vendorKey, id string) error {
	f.calls = append(f.calls, fmt.Sprintf("delete %s/%s", vendorKey, id))
	return nil
}

func (f *fakeService) SaveAuth(_ context.Context, vendorKey, id string, req client.AuthSaveRequest) error {
	f.calls = append(f.calls, fmt.Sprintf("saveauth %s/%s %s", vendorKey, id, req.AuthKey))
	return nil
}

func (f *fakeService) ValidateAuth(_ context.Context, vendorKey, id string, req client.AuthSaveRequest) (*client.ValidateResult, error) {
	f.calls = append(f.calls, fmt.Sprintf("validate %s/%s %s", vendorKey, id, req.AuthKey))
	return &client.ValidateResult{IsValid: true}, nil
}

func (f *fakeService) SelectKind(_ context.Context, vendorKey, id, kind string, selected bool) error {
	f.calls = append(f.calls, fmt.Sprintf("selectkind %s/%s %s=%v", vendorKey, id, kind, selected))
	return nil
}

func (f *fakeService) SaveKind(_ context.Context, vendorKey, id, kind string, params []map[string]any) error {
	f.calls = append(f.calls, fmt.Sprintf("savekind %s/%s %s (%d fields)", vendorKey, id, kind, len(params)))
	return nil
}

func (f *fakeService) Connect(_ context.Context, vendorKey, id string) (*client.ConnectStatus, error) {
	f.calls = append(f.calls, fmt.Sprintf("connect %s/%s", vendorKey, id))
	return &client.ConnectStatus{ID: id, Status: "connecting"}, nil
}

func (f *fakeService) GetVendor(_ context.Context, vendorKey string) (*client.Vendor, error) {
	f.calls = append(f.calls, fmt.Sprintf("vendor %s", vendorKey))
	if f.vendorErr != nil {
		return nil, f.vendorErr
	}
	return &client.Vendor{VendorKey: vendorKey}, nil
}

func TestScalarsNonEmpty(t *testing.T) {
	if scalarsNonEmpty(client.ScalarUpdateRequest{}) {
		t.Fatal("empty request should be empty")
	}
	name := "x"
	if !scalarsNonEmpty(client.ScalarUpdateRequest{ConnectionName: &name}) {
		t.Fatal("set field should be non-empty")
	}
}

func TestImportIDSplit(t *testing.T) {
	cases := []struct {
		id      string
		wantErr bool
	}{
		{"aws_s3/5", false},
		{"aws_s3", true},
		{"aws_s3/5/extra", true},
		{"/5", true},
		{"aws_s3/", true},
		{"", true},
	}
	for _, tc := range cases {
		parts := splitImportID(tc.id)
		if tc.wantErr != (parts == nil) {
			t.Fatalf("id %q: parts=%v wantErr=%v", tc.id, parts, tc.wantErr)
		}
	}
}

func TestReadBackNotFound(t *testing.T) {
	f := &fakeService{getErr: common.ErrNotFound}
	r := &connectionResource{svc: f}
	m := &resourceModel{}
	ok, _ := r.readBack(context.Background(), m)
	if ok {
		t.Fatal("expected read-back failure on not-found")
	}
}
