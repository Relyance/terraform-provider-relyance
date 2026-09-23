// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package client

// Wire types match integrations-api's serializers
// (src/services/connections/v1/serializers.py) field-for-field. JSON tags are
// the contract — change them only against the server source.

// CreateConnectionRequest mirrors CreateConnectionRequest.
type CreateConnectionRequest struct {
	ConnectionName  string  `json:"connectionName"`
	IntegrationType *string `json:"integrationType,omitempty"`
}

// ScalarUpdateRequest mirrors ScalarUpdateRequest: every field optional;
// omitted fields are left untouched server-side.
type ScalarUpdateRequest struct {
	ConnectionName       *string           `json:"connectionName,omitempty"`
	RefreshFrequency     *int64            `json:"refreshFrequency,omitempty"`
	StartScanFrom        *float64          `json:"startScanFrom,omitempty"`
	DataStorageLocation  map[string]string `json:"dataStorageLocation,omitempty"`
	BusinessNodeIDs      []string          `json:"businessNodeIds,omitempty"`
	CredentialsExpireAt  *float64          `json:"credentialsExpireAt,omitempty"`
	RelyanceSecretAccess *bool             `json:"relyanceSecretAccess,omitempty"`
}

// AuthSaveRequest mirrors AuthSaveRequest.
//
// SecretRef is the InHost BYOK external secret reference (an AWS Secrets
// Manager ARN). It is tri-state on the wire: nil omits the field (the server
// keeps the stored reference), a pointer to "" clears it, and any other value
// sets it.
type AuthSaveRequest struct {
	AuthKey     string         `json:"authKey"`
	CustomCreds map[string]any `json:"customCreds"`
	SecretRef   *string        `json:"secret_ref,omitempty"`
}

// ValidateResult mirrors AuthValidateResponseSerializer.
type ValidateResult struct {
	IsValid      bool             `json:"isValid"`
	Error        *string          `json:"error"`
	FieldResults []map[string]any `json:"fieldResults"`
}

// ConnectionSummary is one element of the list response. The connection
// sub-document is served with passthrough extras (vendor-shaped), so only the
// identity field is typed; everything else stays in Raw.
type ConnectionSummary struct {
	ID  string
	Raw map[string]any
}

// ConnectionDetail mirrors the detail response envelope
// {connection, authConfigs, kindConfigs}. The connection sub-document uses
// extra='allow' passthrough server-side — model it as a map with typed
// accessors rather than a rigid struct.
type ConnectionDetail struct {
	Connection  map[string]any   `json:"connection"`
	AuthConfigs []map[string]any `json:"authConfigs"`
	KindConfigs []map[string]any `json:"kindConfigs"`
}

// RuntimeMode returns the connection's runtime_mode. The server omits it for
// the default, so an absent value is reported as RuntimeModeRelyanceHosted.
func (d *ConnectionDetail) RuntimeMode() string {
	if v, ok := d.Connection["runtime_mode"].(string); ok && v != "" {
		return v
	}
	return RuntimeModeRelyanceHosted
}

// SecretRef returns the connection's auth.secret_ref (the InHost BYOK external
// secret ARN). ok is false when it is absent, null, or empty.
func (d *ConnectionDetail) SecretRef() (string, bool) {
	v, ok := d.Auth()["secret_ref"].(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// Runtime modes a connection can have (the connection's runtime_mode).
const (
	RuntimeModeRelyanceHosted = "RELYANCE_HOSTED"
	RuntimeModeInHostBYOK     = "IN_HOST_BYOK"
)

// Auth returns the connection's auth sub-document (nil if never configured).
func (d *ConnectionDetail) Auth() map[string]any {
	a, _ := d.Connection["auth"].(map[string]any)
	return a
}

// StringField reads a string field off the connection sub-document.
func (d *ConnectionDetail) StringField(key string) (string, bool) {
	v, ok := d.Connection[key].(string)
	return v, ok
}

// NumberField reads a numeric field off the connection sub-document
// (JSON numbers decode as float64).
func (d *ConnectionDetail) NumberField(key string) (float64, bool) {
	v, ok := d.Connection[key].(float64)
	return v, ok
}

// BoolField reads a boolean field off the connection sub-document.
func (d *ConnectionDetail) BoolField(key string) (bool, bool) {
	v, ok := d.Connection[key].(bool)
	return v, ok
}

// StringSliceField reads a []string field off the connection sub-document.
func (d *ConnectionDetail) StringSliceField(key string) ([]string, bool) {
	raw, ok := d.Connection[key].([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// MapStringField reads a map[string]string field off the connection sub-document
// (JSON objects decode as map[string]any).
func (d *ConnectionDetail) MapStringField(key string) (map[string]string, bool) {
	raw, ok := d.Connection[key].(map[string]any)
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out[k] = s
	}
	return out, true
}

// Vendor is one catalog entry from GET /catalog/v1/vendors[/{key}].
type Vendor struct {
	VendorKey   string       `json:"vendorKey"`
	Type        string       `json:"type"`
	AuthConfigs []AuthConfig `json:"authConfigs"`
	Raw         map[string]any
}

// AuthConfig is one auth form option for a vendor. Slug is the
// customer-facing identifier (use it as the connection's auth_key); Key is
// the internal legacy identifier, still accepted for compatibility.
type AuthConfig struct {
	Name         string        `json:"name"`
	Key          string        `json:"key"`
	Slug         string        `json:"slug"`
	DisplayName  string        `json:"displayName"`
	CustomFields []CustomField `json:"customFields"`
}

// CustomField is one field in an auth form.
//
// The top-level flag marks fields the server stores as plain values on the
// connection (e.g. data_storage_location) instead of in a secret. The catalog
// omits false flags, and the flag has been spelled both isTopLevel and
// is_top_level, so both spellings are decoded; use TopLevel() to read it.
type CustomField struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	DefaultValue    string `json:"defaultValue"`
	IsThisSecret    bool   `json:"isThisSecret"`
	FieldType       string `json:"fieldType"`
	IsTopLevel      bool   `json:"isTopLevel"`
	IsTopLevelSnake bool   `json:"is_top_level"`
}

// TopLevel reports whether the field is stored as a plain value on the
// connection rather than as a credential.
func (f CustomField) TopLevel() bool { return f.IsTopLevel || f.IsTopLevelSnake }

// ConnectStatus mirrors the 202 monitor body from POST .../connect.
type ConnectStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
