// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/relyance/terraform-provider-relyance/internal/common"
)

type recorded struct {
	method string
	path   string
	body   string
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		if r.Body != nil {
			buf, _ := io.ReadAll(r.Body)
			// The generated client encodes request bodies with json.Encoder,
			// which appends a trailing newline; trim it so exact-body asserts
			// compare the JSON, not the framing.
			rec.body = strings.TrimRight(string(buf), "\n")
		}
		// Default to JSON so the generated client decodes the mock body; error
		// handlers that need problem+json override this before writing.
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, srv.Client()), rec
}

func TestCreateConnection(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/api/integrations/connections/v1/aws_s3/5")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "5", "connection_name": "x"})
	})
	id, err := c.CreateConnection(context.Background(), "aws_s3", CreateConnectionRequest{ConnectionName: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "5" {
		t.Fatalf("id = %q", id)
	}
	if rec.method != http.MethodPost || rec.path != "/api/integrations/connections/v1/aws_s3" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	// Exact wire contract with the server serializer.
	if rec.body != `{"connectionName":"x"}` {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestUpdateScalarsSendsOnlySetFields(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	name := "renamed"
	var freq int64 = 3600
	err := c.UpdateConnectionScalars(context.Background(), "aws_s3", "5", ScalarUpdateRequest{
		ConnectionName: &name, RefreshFrequency: &freq,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodPatch || rec.path != "/api/integrations/connections/v1/aws_s3/5" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if rec.body != `{"connectionName":"renamed","refreshFrequency":3600}` {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestGetConnectionDetailAccessors(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connection": map[string]any{
				"id": "0", "connection_name": "Conn 1", "refreshFrequency": 31104000,
				"relyanceSecretAccess": true,
				"business_node_ids":    []string{"a", "b"},
				"auth":                 map[string]any{"type": "AUTH_TYPE_CUSTOM", "status": "AUTH_STATUS_CONNECTED"},
			},
			"authConfigs": []map[string]any{{"key": "AUTH_TYPE_CUSTOM"}},
			"kindConfigs": []map[string]any{},
		})
	})
	d, err := c.GetConnection(context.Background(), "aws_s3", "0")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := d.StringField("connection_name"); !ok || v != "Conn 1" {
		t.Fatalf("connection_name = %q %v", v, ok)
	}
	if v, ok := d.NumberField("refreshFrequency"); !ok || v != 31104000 {
		t.Fatalf("refreshFrequency = %v %v", v, ok)
	}
	if v, ok := d.BoolField("relyanceSecretAccess"); !ok || !v {
		t.Fatalf("relyanceSecretAccess = %v %v", v, ok)
	}
	if ids, ok := d.StringSliceField("business_node_ids"); !ok || len(ids) != 2 {
		t.Fatalf("business_node_ids = %v %v", ids, ok)
	}
	if a := d.Auth(); a["status"] != "AUTH_STATUS_CONNECTED" {
		t.Fatalf("auth = %v", a)
	}
}

func TestNotFoundMapsToSentinel(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"Vendor not found: nope"}`))
	})
	_, err := c.GetConnection(context.Background(), "nope", "0")
	if !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestProblemJSONSurfacedInError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Validation failed","status":422,"detail":"account_id is required"}`))
	})
	err := c.SaveAuth(context.Background(), "aws_s3", "5", AuthSaveRequest{AuthKey: "AUTH_TYPE_CUSTOM", CustomCreds: map[string]any{}})
	if err == nil {
		t.Fatal("expected error")
	}
	if want := "account_id is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not surface problem detail %q", err.Error(), want)
	}
}

func TestValidateAuth(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"isValid": false, "error": "bad", "fieldResults": []map[string]any{{"key": "account_id"}}})
	})
	out, err := c.ValidateAuth(context.Background(), "aws_s3", "5", AuthSaveRequest{AuthKey: "AUTH_TYPE_CUSTOM", CustomCreds: map[string]any{"account_id": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.IsValid || out.Error == nil || *out.Error != "bad" || len(out.FieldResults) != 1 {
		t.Fatalf("out = %+v", out)
	}
	if rec.path != "/api/integrations/connections/v1/aws_s3/5/auth/validate" {
		t.Fatalf("path = %s", rec.path)
	}
}

func TestVendorSecretFieldKeys(t *testing.T) {
	v := Vendor{AuthConfigs: []AuthConfig{{
		Key: "AUTH_TYPE_CUSTOM",
		CustomFields: []CustomField{
			{Key: "subdomain"},
			{Key: "api_key", IsThisSecret: true},
		},
	}}}
	keys := v.SecretFieldKeys("AUTH_TYPE_CUSTOM")
	if len(keys) != 1 || keys[0] != "api_key" {
		t.Fatalf("keys = %v", keys)
	}
	if v.SecretFieldKeys("AUTH_TYPE_OAUTH2") != nil {
		t.Fatal("unknown auth key should return nil")
	}
}
