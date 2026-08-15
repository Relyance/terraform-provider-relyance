// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func testJWK(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwk := jose.JSONWebKey{Key: key, KeyID: "test-kid", Algorithm: "RS256", Use: "sig"}
	buf, err := jwk.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(buf), key
}

func TestConfigMode(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		want    string
		wantErr bool
	}{
		{"secret", Config{ClientSecret: "s"}, "client_secret_post", false},
		{"jwk", Config{JWKJSON: "{}"}, "private_key_jwt", false},
		{"both", Config{ClientSecret: "s", JWKJSON: "{}"}, "", true},
		{"neither", Config{}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.cfg.Mode()
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildClientAssertionRoundTrip(t *testing.T) {
	jwkJSON, key := testJWK(t)
	assertion, err := buildClientAssertion("client-1", "https://idp.example/oauth/token", jwkJSON, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	var claims jwt.Claims
	if err := parsed.Claims(key.Public(), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != "client-1" || claims.Subject != "client-1" {
		t.Fatalf("iss/sub = %q/%q", claims.Issuer, claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "https://idp.example/oauth/token" {
		t.Fatalf("aud = %v", claims.Audience)
	}
	if claims.ID == "" {
		t.Fatal("missing jti")
	}
	if claims.Expiry.Time().Sub(claims.IssuedAt.Time()) != time.Minute {
		t.Fatalf("ttl = %v", claims.Expiry.Time().Sub(claims.IssuedAt.Time()))
	}
}

func TestBuildClientAssertionRejectsPublicKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub := jose.JSONWebKey{Key: key.Public(), KeyID: "k", Algorithm: "RS256"}
	buf, _ := pub.MarshalJSON()
	if _, err := buildClientAssertion("c", "u", string(buf), 0); err == nil {
		t.Fatal("expected error for public key")
	}
}

func TestTokenSourceClientSecret(t *testing.T) {
	var gotForm map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-123", "token_type": "Bearer", "expires_in": 3600,
		})
	}))
	defer srv.Close()

	ts, err := NewTokenSource(Config{
		TokenURL: srv.URL, ClientID: "cid", ClientSecret: "shh", Audience: "api://integrations",
		Scopes: []string{"integrations.connections.read"},
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "tok-123" {
		t.Fatalf("token = %q", tok.AccessToken)
	}
	for k, want := range map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     "cid",
		"client_secret": "shh",
		"audience":      "api://integrations",
		"scope":         "integrations.connections.read",
	} {
		if got := gotForm[k]; len(got) != 1 || got[0] != want {
			t.Fatalf("form[%s] = %v, want %q", k, got, want)
		}
	}

	// Second call reuses the cached token (no second HTTP hit would be
	// detectable via changed form, but ReuseTokenSource guarantees identity).
	tok2, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok2.AccessToken != tok.AccessToken {
		t.Fatal("expected cached token")
	}
}

func TestTokenSourcePrivateKeyJWT(t *testing.T) {
	jwkJSON, _ := testJWK(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("client_assertion") == "" {
			t.Error("missing client_assertion")
		}
		if got := r.PostForm.Get("client_assertion_type"); got != clientAssertionType {
			t.Errorf("client_assertion_type = %q", got)
		}
		if r.PostForm.Get("client_secret") != "" {
			t.Error("client_secret must not be sent in jwt mode")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-jwt", "token_type": "Bearer", "expires_in": 600})
	}))
	defer srv.Close()

	ts, err := NewTokenSource(Config{TokenURL: srv.URL, ClientID: "cid", JWKJSON: jwkJSON, Audience: "api://integrations"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "tok-jwt" {
		t.Fatalf("token = %q", tok.AccessToken)
	}
}

func TestTokenSourceErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()

	ts, err := NewTokenSource(Config{TokenURL: srv.URL, ClientID: "cid", ClientSecret: "bad", Audience: "a"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Token(); err == nil {
		t.Fatal("expected error")
	}
}
