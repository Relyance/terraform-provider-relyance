// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestErrWithHTTPRedactsSensitiveValues(t *testing.T) {
	body := `{"detail":"Bearer secret-token","jwt":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhIjoiYiJ9.signature","api_key":"supersecretapikeyvaluewithlength","email":"ops@example.com"}`
	reqURL, _ := url.Parse("https://api.example.test/v1/resource")
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Status:     http.StatusText(http.StatusUnauthorized),
		Header: http.Header{
			"Content-Type":     []string{"application/json"},
			"X-Correlation-Id": []string{"req-123"},
		},
		Body:    io.NopCloser(bytes.NewBufferString(body)),
		Request: &http.Request{Method: http.MethodPost, URL: reqURL},
	}

	baseErr := errors.New("upstream failed")
	wrapped := ErrWithHTTP(resp, baseErr)

	if !errors.Is(wrapped, baseErr) {
		t.Fatalf("ErrWithHTTP should wrap the original error")
	}
	msg := wrapped.Error()
	// Ensure original sensitive payload is redacted.
	if strings.Contains(msg, "secret-token") || strings.Contains(msg, "supersecretapikeyvaluewithlength") {
		t.Fatalf("wrapped error did not redact secrets: %s", msg)
	}
	if strings.Contains(msg, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Fatalf("wrapped error did not redact JWT: %s", msg)
	}
	if !strings.Contains(msg, "[REDACTED_API_KEY]") || !strings.Contains(msg, "[REDACTED_JWT]") || !strings.Contains(msg, `"email":"[REDACTED]"`) {
		t.Fatalf("wrapped error did not include redaction markers: %s", msg)
	}
	if !strings.Contains(msg, "req_id=req-123") {
		t.Fatalf("wrapped error must include correlation id: %s", msg)
	}
}

func TestErrWithHTTPNilResponse(t *testing.T) {
	baseErr := errors.New("plain error")
	if err := ErrWithHTTP(nil, baseErr); !errors.Is(err, baseErr) {
		t.Fatalf("expected the base error when response is nil, got %v", err)
	}
}

func TestErrWithHTTPProblemJSON(t *testing.T) {
	body := `{"title":"Conflict","detail":"Bearer should not leak","status":409,"code":"conflict"}`
	resp := &http.Response{
		StatusCode: http.StatusConflict,
		Status:     http.StatusText(http.StatusConflict),
		Header: http.Header{
			"Content-Type": []string{"application/problem+json"},
		},
		Body: io.NopCloser(bytes.NewBufferString(body)),
	}
	baseErr := errors.New("conflict")

	err := ErrWithHTTP(resp, baseErr)
	if !errors.Is(err, baseErr) {
		t.Fatalf("ErrWithHTTP should wrap base error for problem+json responses")
	}
	msg := err.Error()
	if !strings.Contains(msg, "problem{title=\"Conflict\"") {
		t.Fatalf("expected problem details in wrapped error, got %s", msg)
	}
	if strings.Contains(msg, "Bearer should not leak") {
		t.Fatalf("problem detail should be sanitized, got %s", msg)
	}
}

func TestSanitizeStandalone(t *testing.T) {
	got := sanitize(`Bearer token eyJabc.def.ghi api_key:"ABCdef1234567890GHIJKL" "email":"developer@example.com"`)
	if strings.Contains(got, "developer@example.com") {
		t.Fatalf("email should have been redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, "[REDACTED_JWT]") || !strings.Contains(got, "[REDACTED_API_KEY]") {
		t.Fatalf("expected redaction markers, got %s", got)
	}
}

// client_secret is the primary OAuth provider-credential field name here, so it
// must be redacted (and Sanitize is exported for the token source to call).
func TestSanitizeRedactsClientSecret(t *testing.T) {
	secret := "abcdef0123456789ABCDEF01"
	got := Sanitize(`{"error":"invalid_client","client_secret":"` + secret + `"}`)
	if strings.Contains(got, secret) {
		t.Fatalf("client_secret should have been redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED_API_KEY]") {
		t.Fatalf("expected redaction marker, got %s", got)
	}
}

func TestProblemErrorError(t *testing.T) {
	pe := ProblemError{Status: "409 Conflict", Body: "already exists"}
	if got := pe.Error(); got != "409 Conflict: already exists" {
		t.Fatalf("unexpected error string %q", got)
	}
}
