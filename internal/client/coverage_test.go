// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/relyance/terraform-provider-relyance/internal/common"
)

// --- businessnodes pagination ---------------------------------------------

func TestListBusinessNodesMultiPageAccumulates(t *testing.T) {
	var requests []string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requests = append(requests, r.URL.RequestURI())
		calls++
		switch calls {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"record_id": "1", "name": "Alice", "type": "TYPE_BUSINESS_ENTITY"},
					{"record_id": "2", "name": "Bob", "type": "TYPE_BUSINESS_ENTITY"},
				},
				"total_count": 3,
				"page_num":    1, "page_size": 1000,
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results":     []map[string]any{{"record_id": "3", "name": "Carol", "type": "TYPE_BUSINESS_ENTITY"}},
				"total_count": 3,
				"page_num":    1, "page_size": 1000,
			})
		default:
			t.Fatalf("unexpected extra request #%d: %s", calls, r.URL.RequestURI())
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, srv.Client())
	nodes, err := c.ListBusinessNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("len(nodes) = %d, want 3", len(nodes))
	}
	if nodes[0].ID != "1" || nodes[1].ID != "2" || nodes[2].ID != "3" {
		t.Fatalf("ids out of order: %+v", nodes)
	}
	if nodes[2].Name != "Carol" || nodes[2].Type != "TYPE_BUSINESS_ENTITY" {
		t.Fatalf("last node = %+v", nodes[2])
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (should stop once total_count reached)", calls)
	}
	if requests[0] != "/api/integrations/classification/v1/business-nodes?page_num=1&page_size=1000" {
		t.Fatalf("page 1 request = %s", requests[0])
	}
	if requests[1] != "/api/integrations/classification/v1/business-nodes?page_num=2&page_size=1000" {
		t.Fatalf("page 2 request = %s", requests[1])
	}
}

func TestListBusinessNodesSinglePageIsTerminal(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results":     []map[string]any{{"record_id": "1", "name": "Only", "type": "TYPE_PRODUCT"}},
			"total_count": 1,
			"page_num":    1, "page_size": 1000,
		})
	})
	nodes, err := c.ListBusinessNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].ID != "1" {
		t.Fatalf("nodes = %+v", nodes)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (single page already satisfies total_count)", calls)
	}
}

func TestListBusinessNodesStopsOnEmptyPage(t *testing.T) {
	// Server mis-reports total_count higher than it ever delivers; the loop
	// must still terminate once a page comes back empty rather than spin.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results":     []map[string]any{{"record_id": "1", "name": "Only", "type": "TYPE_PRODUCT"}},
				"total_count": 5,
				"page_num":    1, "page_size": 1000,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}, "total_count": 5, "page_num": 2, "page_size": 1000})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, srv.Client())
	nodes, err := c.ListBusinessNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %+v", nodes)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

// --- catalog ---------------------------------------------------------------

// escRecorded captures the wire-exact escaped path (net/http decodes
// r.URL.Path, which would hide %20 vs literal-space bugs) alongside the
// method, for tests that specifically assert url.PathEscape behavior.
type escRecorded struct {
	method      string
	escapedPath string
}

func newEscRecordingClient(t *testing.T, handler http.HandlerFunc) (*Client, *escRecorded) {
	t.Helper()
	rec := &escRecorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.escapedPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, srv.Client()), rec
}

func TestGetVendor(t *testing.T) {
	c, rec := newEscRecordingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"vendorKey": "cat vendor",
			"type":      "storage",
			"authConfigs": []map[string]any{
				{
					"name": "Custom", "key": "AUTH_TYPE_CUSTOM", "slug": "custom", "displayName": "Custom Auth",
					"customFields": []map[string]any{
						{"key": "account_id", "name": "Account ID", "defaultValue": "", "isThisSecret": false, "fieldType": "string"},
						{"key": "api_key", "name": "API Key", "defaultValue": "", "isThisSecret": true, "fieldType": "string"},
					},
				},
			},
			"extraCatalogField": "kept-in-raw",
		})
	})
	v, err := c.GetVendor(context.Background(), "cat vendor")
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodGet {
		t.Fatalf("method = %s", rec.method)
	}
	if got, want := rec.escapedPath, "/api/integrations/catalog/v1/vendors/cat%20vendor"; got != want {
		t.Fatalf("escaped path = %s, want %s", got, want)
	}
	if v.VendorKey != "cat vendor" || v.Type != "storage" {
		t.Fatalf("v = %+v", v)
	}
	if len(v.AuthConfigs) != 1 || v.AuthConfigs[0].Slug != "custom" || len(v.AuthConfigs[0].CustomFields) != 2 {
		t.Fatalf("authConfigs = %+v", v.AuthConfigs)
	}
	if v.Raw["extraCatalogField"] != "kept-in-raw" {
		t.Fatalf("raw passthrough missing: %+v", v.Raw)
	}
	// Cross-check against the typed SecretFieldKeys helper exercised in client_test.go.
	if keys := v.SecretFieldKeys("AUTH_TYPE_CUSTOM"); len(keys) != 1 || keys[0] != "api_key" {
		t.Fatalf("SecretFieldKeys = %v", keys)
	}
}

func TestListVendors(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"vendorKey": "aws_s3", "type": "storage", "authConfigs": []map[string]any{}},
			{"vendorKey": "jira", "type": "saas", "authConfigs": []map[string]any{}},
		})
	})
	vendors, err := c.ListVendors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodGet || rec.path != "/api/integrations/catalog/v1/vendors" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if len(vendors) != 2 || vendors[0].VendorKey != "aws_s3" || vendors[1].VendorKey != "jira" {
		t.Fatalf("vendors = %+v", vendors)
	}
}

func TestListVendorsNonSuccessWrapsStatus(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Service Unavailable","status":503,"detail":"catalog backend down"}`))
	})
	_, err := c.ListVendors(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, common.ErrNotFound) {
		t.Fatal("503 must not map to ErrNotFound")
	}
	msg := err.Error()
	if !strings.Contains(msg, "503") {
		t.Fatalf("error %q does not surface status 503", msg)
	}
	if !strings.Contains(msg, "catalog backend down") {
		t.Fatalf("error %q does not surface problem detail", msg)
	}
}

// --- connections -------------------------------------------------------

func TestListConnectionsSuccess(t *testing.T) {
	c, rec := newEscRecordingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "1", "connection_name": "Prod"},
			{"id": "2", "connection_name": "Dev"},
		})
	})
	conns, err := c.ListConnections(context.Background(), "aws s3")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rec.escapedPath, "/api/integrations/connections/v1/aws%20s3"; got != want {
		t.Fatalf("escaped path = %s, want %s", got, want)
	}
	if len(conns) != 2 {
		t.Fatalf("conns = %+v", conns)
	}
	if conns[0].ID != "1" || conns[0].Raw["connection_name"] != "Prod" {
		t.Fatalf("conns[0] = %+v", conns[0])
	}
	if conns[1].ID != "2" || conns[1].Raw["connection_name"] != "Dev" {
		t.Fatalf("conns[1] = %+v", conns[1])
	}
}

func TestListConnectionsNotFoundMapsToSentinel(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"Vendor not found: nope"}`))
	})
	conns, err := c.ListConnections(context.Background(), "nope")
	if conns != nil {
		t.Fatalf("expected nil connections on error, got %+v", conns)
	}
	if !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGetConnectionEscapesVendorAndID(t *testing.T) {
	c, rec := newEscRecordingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connection":  map[string]any{"id": "id two"},
			"authConfigs": []map[string]any{},
			"kindConfigs": []map[string]any{},
		})
	})
	d, err := c.GetConnection(context.Background(), "vendor two", "id two")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rec.escapedPath, "/api/integrations/connections/v1/vendor%20two/id%20two"; got != want {
		t.Fatalf("escaped path = %s, want %s", got, want)
	}
	if v, ok := d.StringField("id"); !ok || v != "id two" {
		t.Fatalf("id field = %q %v", v, ok)
	}
}

func TestCreateConnectionWithIntegrationType(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/api/integrations/connections/v1/aws_s3/9")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "9"})
	})
	itype := "readonly"
	id, err := c.CreateConnection(context.Background(), "aws_s3", CreateConnectionRequest{
		ConnectionName: "prod", IntegrationType: &itype,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "9" {
		t.Fatalf("id = %q", id)
	}
	if rec.body != `{"connectionName":"prod","integrationType":"readonly"}` {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestCreateConnectionMissingIDIsError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"connection_name": "no-id-field"})
	})
	id, err := c.CreateConnection(context.Background(), "aws_s3", CreateConnectionRequest{ConnectionName: "x"})
	if err == nil {
		t.Fatal("expected error when response has no id")
	}
	if id != "" {
		t.Fatalf("id = %q, want empty on error", id)
	}
}

func TestUpdateConnectionScalarsAllFields(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	name := "renamed"
	var freq int64 = 7200
	startScan := 1700000000.0
	loc := map[string]string{"region": "us"}
	credsExpire := 1800000000.0
	secretAccess := true
	err := c.UpdateConnectionScalars(context.Background(), "aws_s3", "5", ScalarUpdateRequest{
		ConnectionName:       &name,
		RefreshFrequency:     &freq,
		StartScanFrom:        &startScan,
		DataStorageLocation:  loc,
		BusinessNodeIDs:      []string{"a", "b"},
		CredentialsExpireAt:  &credsExpire,
		RelyanceSecretAccess: &secretAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(rec.body), &body); err != nil {
		t.Fatalf("body not valid JSON: %s", rec.body)
	}
	want := map[string]any{
		"connectionName":       "renamed",
		"refreshFrequency":     float64(7200),
		"startScanFrom":        1700000000.0,
		"dataStorageLocation":  map[string]any{"region": "us"},
		"businessNodeIds":      []any{"a", "b"},
		"credentialsExpireAt":  1800000000.0,
		"relyanceSecretAccess": true,
	}
	gotJSON, _ := json.Marshal(body)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("body = %s, want %s", gotJSON, wantJSON)
	}
}

func TestDeleteConnectionSuccess(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.DeleteConnection(context.Background(), "aws_s3", "5"); err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodDelete || rec.path != "/api/integrations/connections/v1/aws_s3/5" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
}

func TestDeleteConnectionNotFoundMapsToSentinel(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	err := c.DeleteConnection(context.Background(), "aws_s3", "missing")
	if !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// --- auth --------------------------------------------------------------

func TestSaveAuthSuccess(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	err := c.SaveAuth(context.Background(), "aws_s3", "5", AuthSaveRequest{
		AuthKey:     "AUTH_TYPE_CUSTOM",
		CustomCreds: map[string]any{"account_id": "123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPut || rec.path != "/api/integrations/connections/v1/aws_s3/5/auth" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if rec.body != `{"authKey":"AUTH_TYPE_CUSTOM","customCreds":{"account_id":"123"}}` {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestConnectSuccess(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "5", "status": "AUTH_STATUS_PENDING"})
	})
	out, err := c.Connect(context.Background(), "aws_s3", "5")
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPost || rec.path != "/api/integrations/connections/v1/aws_s3/5/connect" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if out.ID != "5" || out.Status != "AUTH_STATUS_PENDING" {
		t.Fatalf("out = %+v", out)
	}
}

func TestConnectNonSuccessWrapsStatus(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"title":"Internal Error","status":500,"detail":"credential test crashed"}`))
	})
	out, err := c.Connect(context.Background(), "aws_s3", "5")
	if out != nil {
		t.Fatalf("expected nil result on error, got %+v", out)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, common.ErrNotFound) {
		t.Fatal("500 must not map to ErrNotFound")
	}
	msg := err.Error()
	if !strings.Contains(msg, "500") {
		t.Fatalf("error %q does not surface status 500", msg)
	}
	if !strings.Contains(msg, "credential test crashed") {
		t.Fatalf("error %q does not surface problem detail", msg)
	}
}

func TestDisconnectSuccess(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.Disconnect(context.Background(), "aws_s3", "5"); err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPost || rec.path != "/api/integrations/connections/v1/aws_s3/5/disconnect" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if rec.body != "" {
		t.Fatalf("disconnect must send no body, got %q", rec.body)
	}
}

// --- kinds ---------------------------------------------------------------

func TestSelectKind(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	err := c.SelectKind(context.Background(), "aws_s3", "5", "s3_bucket", true)
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPut || rec.path != "/api/integrations/connections/v1/aws_s3/5/kinds/s3_bucket/selection" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if rec.body != `{"selected":true}` {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestSelectKindDeselect(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	err := c.SelectKind(context.Background(), "aws_s3", "5", "s3_bucket", false)
	if err != nil {
		t.Fatal(err)
	}
	if rec.body != `{"selected":false}` {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestSaveKind(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	params := []map[string]any{{"key": "bucket_name", "value": "my-bucket"}}
	err := c.SaveKind(context.Background(), "aws_s3", "5", "s3_bucket", params)
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPut || rec.path != "/api/integrations/connections/v1/aws_s3/5/kinds/s3_bucket" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if rec.body != `{"parameters":[{"key":"bucket_name","value":"my-bucket"}]}` {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestSaveKindNotFoundMapsToSentinel(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	err := c.SaveKind(context.Background(), "aws_s3", "missing", "s3_bucket", nil)
	if !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListBusinessNodesToleratesUnknownType(t *testing.T) {
	// Forward-compat regression guard: the server adding a NEW business-node
	// type (a purely additive change) must not fail deserialization in
	// already-shipped providers. The vendored spec deliberately serves `type`
	// as an open string, not a closed enum — see internal/apiclient/openapi.json.
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"record_id": "1", "name": "Known", "type": "TYPE_BUSINESS_ENTITY"},
				{"record_id": "2", "name": "Novel", "type": "TYPE_ADDED_SERVER_SIDE_LATER"},
			},
			"total_count": 2, "page_num": 1, "page_size": 1000,
		})
	})
	nodes, err := c.ListBusinessNodes(context.Background())
	if err != nil {
		t.Fatalf("unknown type must not fail the page: %v", err)
	}
	if len(nodes) != 2 || nodes[1].Type != "TYPE_ADDED_SERVER_SIDE_LATER" {
		t.Fatalf("nodes = %+v", nodes)
	}
}
