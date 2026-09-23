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

func TestSaveAuthSecretRefTriState(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:relyance/jira-AbCdEf"
	empty := ""
	ref := arn
	cases := []struct {
		name string
		ref  *string
		want string
	}{
		{"absent leaves it unchanged", nil, `{"authKey":"api-key","customCreds":{"data_storage_location":"us"}}`},
		{"empty clears it", &empty, `{"authKey":"api-key","customCreds":{"data_storage_location":"us"},"secret_ref":""}`},
		{"value sets it", &ref, `{"authKey":"api-key","customCreds":{"data_storage_location":"us"},"secret_ref":"` + arn + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			err := c.SaveAuth(context.Background(), "atlassian_jira", "3", AuthSaveRequest{
				AuthKey:     "api-key",
				CustomCreds: map[string]any{"data_storage_location": "us"},
				SecretRef:   tc.ref,
			})
			if err != nil {
				t.Fatal(err)
			}
			if rec.body != tc.want {
				t.Fatalf("body = %s, want %s", rec.body, tc.want)
			}
		})
	}
}

func TestValidateAuthSendsSecretRef(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:x"
	c, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"isValid": true})
	})
	ref := arn
	if _, err := c.ValidateAuth(context.Background(), "atlassian_jira", "3", AuthSaveRequest{AuthKey: "api-key", SecretRef: &ref}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.body, `"secret_ref":"`+arn+`"`) {
		t.Fatalf("body = %s", rec.body)
	}
}

func TestConnectionDetailRuntimeModeAndSecretRef(t *testing.T) {
	d := &ConnectionDetail{Connection: map[string]any{}}
	if d.RuntimeMode() != RuntimeModeRelyanceHosted {
		t.Fatalf("absent runtime_mode = %s", d.RuntimeMode())
	}
	if _, ok := d.SecretRef(); ok {
		t.Fatal("no auth → no secret_ref")
	}
	d.Connection["runtime_mode"] = RuntimeModeInHostBYOK
	d.Connection["auth"] = map[string]any{"secret_ref": "arn:aws:secretsmanager:us-east-1:123456789012:secret:x"}
	if d.RuntimeMode() != RuntimeModeInHostBYOK {
		t.Fatalf("runtime_mode = %s", d.RuntimeMode())
	}
	if v, ok := d.SecretRef(); !ok || v == "" {
		t.Fatalf("secret_ref = %q %v", v, ok)
	}
	d.Connection["auth"] = map[string]any{"secret_ref": nil}
	if _, ok := d.SecretRef(); ok {
		t.Fatal("null secret_ref must read as absent")
	}
}

func TestCustomFieldTopLevelDecodesBothSpellings(t *testing.T) {
	v, err := vendorFromRaw(map[string]any{
		"vendorKey": "atlassian_jira",
		"authConfigs": []any{map[string]any{
			"key": "AUTH_TYPE_API_KEY", "slug": "api-key",
			"customFields": []any{
				map[string]any{"key": "data_storage_location", "isTopLevel": true},
				map[string]any{"key": "region", "is_top_level": true},
				map[string]any{"key": "API_KEY", "isThisSecret": true},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := v.AuthConfigs[0].CustomFields
	if !f[0].TopLevel() || !f[1].TopLevel() || f[2].TopLevel() {
		t.Fatalf("top-level flags = %v %v %v", f[0].TopLevel(), f[1].TopLevel(), f[2].TopLevel())
	}
}
