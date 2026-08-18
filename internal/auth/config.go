// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package auth mints tenant OAuth-client access tokens from the Relyance
// identity service's public token endpoint (RFC 6749 §4.4 client_credentials),
// authenticating with either a client secret (client_secret_post) or a
// private_key_jwt client assertion (RFC 7523 §2.2).
//
// This is deliberately hand-rolled with no private-module dependencies: the
// provider must remain `go build`-able outside the Relyance org for eventual
// customer distribution, and the token endpoint is plain OAuth.
package auth

import (
	"errors"
	"time"
)

// Config carries everything needed to mint tokens for one provider instance.
type Config struct {
	// TokenURL is the identity service's public token endpoint, e.g.
	// https://beta.api.relyance.ai/oauth/token.
	TokenURL string
	// ClientID identifies the tenant OAuth client.
	ClientID string
	// ClientSecret enables client_secret_post authentication when set.
	ClientSecret string
	// JWKJSON is a JSON-encoded private JWK; enables private_key_jwt
	// authentication when set. Mutually exclusive with ClientSecret.
	JWKJSON string
	// Audience is the target resource server, e.g. api://integrations.
	Audience string
	// Scopes optionally requests specific scope slugs. Authorization at
	// integrations-api is permission-based server-side, so this is usually
	// left empty.
	Scopes []string
	// AssertionTTL bounds the client_assertion lifetime (private_key_jwt).
	AssertionTTL time.Duration
}

// Mode reports which client authentication method the config selects.
func (c Config) Mode() (string, error) {
	switch {
	case c.ClientSecret != "" && c.JWKJSON != "":
		return "", errors.New("client_secret and jwk_json are mutually exclusive")
	case c.ClientSecret != "":
		return "client_secret_post", nil
	case c.JWKJSON != "":
		return "private_key_jwt", nil
	default:
		return "", errors.New("one of client_secret or jwk_json must be configured")
	}
}

// Validate checks the config is complete enough to mint.
func (c Config) Validate() error {
	if c.TokenURL == "" {
		return errors.New("identity_token_url must be configured")
	}
	if c.ClientID == "" {
		return errors.New("client_id must be configured")
	}
	if c.Audience == "" {
		return errors.New("audience must be configured")
	}
	_, err := c.Mode()
	return err
}
