// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const defaultAssertionTTL = 2 * time.Minute

// buildClientAssertion signs a private_key_jwt client assertion (RFC 7523
// §2.2): iss = sub = client_id, aud = token endpoint, single-use jti, short
// expiry. The JWK's own alg/kid are honored so the server can match the
// registered OAuthClientJWK.
func buildClientAssertion(clientID, tokenURL, jwkJSON string, ttl time.Duration) (string, error) {
	var key jose.JSONWebKey
	if err := json.Unmarshal([]byte(jwkJSON), &key); err != nil {
		return "", fmt.Errorf("parsing jwk_json: %w", err)
	}
	if key.IsPublic() {
		return "", fmt.Errorf("jwk_json must be a private key")
	}
	// Honor the JWK's own alg; otherwise infer it from the key type/curve so the
	// signature algorithm matches the key (an EC key must not be signed as RS256).
	alg := key.Algorithm
	if alg == "" {
		switch k := key.Key.(type) {
		case *rsa.PrivateKey:
			alg = "RS256"
		case ed25519.PrivateKey:
			alg = "EdDSA"
		case *ecdsa.PrivateKey:
			switch k.Curve {
			case elliptic.P384():
				alg = "ES384"
			case elliptic.P521():
				alg = "ES512"
			default:
				alg = "ES256"
			}
		default:
			return "", fmt.Errorf("jwk_json has no \"alg\" and key type %T is unsupported; set alg explicitly", key.Key)
		}
	}
	if ttl <= 0 {
		ttl = defaultAssertionTTL
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.SignatureAlgorithm(alg), Key: key.Key},
		(&jose.SignerOptions{}).WithHeader("kid", key.KeyID).WithType("JWT"),
	)
	if err != nil {
		return "", fmt.Errorf("building signer: %w", err)
	}

	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", fmt.Errorf("generating jti: %w", err)
	}
	now := time.Now()
	claims := jwt.Claims{
		Issuer:   clientID,
		Subject:  clientID,
		Audience: jwt.Audience{tokenURL},
		ID:       base64.RawURLEncoding.EncodeToString(jti),
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(ttl)),
	}
	assertion, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("signing client assertion: %w", err)
	}
	return assertion, nil
}
