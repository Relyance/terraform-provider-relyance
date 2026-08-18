// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/relyance/terraform-provider-relyance/internal/common"
)

const clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// NewTokenSource returns a caching oauth2.TokenSource that mints tenant
// OAuth-client access tokens via client_credentials. hc is used for the token
// request itself (retry/UA wrapping happens at the provider layer).
func NewTokenSource(cfg Config, hc *http.Client) (oauth2.TokenSource, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return oauth2.ReuseTokenSource(nil, &tokenSource{cfg: cfg, hc: hc}), nil
}

type tokenSource struct {
	cfg Config
	hc  *http.Client
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (t *tokenSource) Token() (*oauth2.Token, error) {
	// Bound the mint by the caller-configured HTTP timeout (timeout_seconds),
	// falling back to 30s — a hardcoded 30s here would silently override a
	// practitioner who raised timeout_seconds for a slow token endpoint.
	mintTimeout := 30 * time.Second
	if t.hc.Timeout > 0 {
		mintTimeout = t.hc.Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), mintTimeout)
	defer cancel()

	form := url.Values{
		"grant_type": {"client_credentials"},
		"audience":   {t.cfg.Audience},
		"client_id":  {t.cfg.ClientID},
	}
	if len(t.cfg.Scopes) > 0 {
		form.Set("scope", strings.Join(t.cfg.Scopes, " "))
	}
	mode, err := t.cfg.Mode()
	if err != nil {
		return nil, err
	}
	switch mode {
	case "client_secret_post":
		form.Set("client_secret", t.cfg.ClientSecret)
	case "private_key_jwt":
		assertion, err := buildClientAssertion(t.cfg.ClientID, t.cfg.TokenURL, t.cfg.JWKJSON, t.cfg.AssertionTTL)
		if err != nil {
			return nil, err
		}
		form.Set("client_assertion", assertion)
		form.Set("client_assertion_type", clientAssertionType)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, truncateForError(body))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint returned no access_token")
	}
	tok := &oauth2.Token{AccessToken: tr.AccessToken, TokenType: tr.TokenType}
	if tr.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return tok, nil
}

// truncateForError redacts secret material from, and bounds the length of, a
// token-endpoint error body before it reaches a diagnostic. Don't assume the
// endpoint (or an intermediary proxy) never echoes the request's client_secret
// or signed assertion on error -- redact, then truncate.
func truncateForError(b []byte) string {
	const max = 500
	s := common.Sanitize(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
