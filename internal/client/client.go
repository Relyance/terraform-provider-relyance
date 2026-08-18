// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package client is a thin, ergonomic facade over the generated apiclient
// (internal/apiclient, produced from integrations-api's OpenAPI spec via
// `make generate`). It owns only what a generated SDK does not: 404 ->
// ErrNotFound mapping, RFC-7807 error enrichment with secret redaction,
// pagination, and typed accessors for the vendor-shaped connection sub-document.
// The wire contract lives in apiclient and is drift-checked in CI, so a
// server-side field rename or type change breaks this facade at compile time.
package client

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/relyance/terraform-provider-relyance/internal/apiclient"
	"github.com/relyance/terraform-provider-relyance/internal/common"
)

// Client talks to one integrations-api base URL as one tenant (the tenant is
// carried by the bearer token on the transport, not by any request parameter).
type Client struct {
	api *apiclient.APIClient
}

// New builds a Client. baseURL is the host root, e.g.
// https://beta.api.relyance.ai — the generated client appends the
// /api/integrations/... paths from the spec. Transport concerns (auth header,
// retries, user-agent, timeout) belong to hc, wired by the provider's Configure.
func New(baseURL string, hc *http.Client) *Client {
	cfg := apiclient.NewConfiguration()
	cfg.Servers = apiclient.ServerConfigurations{{URL: strings.TrimRight(baseURL, "/")}}
	cfg.HTTPClient = hc
	return &Client{api: apiclient.NewAPIClient(cfg)}
}

// bodyer is satisfied by the generated GenericOpenAPIError (value or pointer);
// it exposes the raw response body the generated client already drained.
type bodyer interface{ Body() []byte }

// apiErr normalizes a generated (resp, err) pair onto the facade's error
// contract: 404 -> common.ErrNotFound (drives Terraform's RemoveResource flow);
// anything else -> common.ErrWithHTTP with the drained body re-attached so the
// existing RFC-7807 parsing + secret redaction still run.
func apiErr(resp *http.Response, err error) error {
	if err == nil {
		return nil
	}
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return common.ErrNotFound
	}
	if resp == nil {
		return err
	}
	var b bodyer
	var body []byte
	if errors.As(err, &b) {
		body = b.Body()
	}
	// The generated client already read + closed resp.Body; rehydrate a copy so
	// ErrWithHTTP can parse/redact it.
	synthetic := &http.Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header,
		Request:    resp.Request,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	return common.ErrWithHTTP(synthetic, err)
}
