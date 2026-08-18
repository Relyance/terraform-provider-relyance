// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/oauth2"

	"github.com/relyance/terraform-provider-relyance/internal/auth"
	"github.com/relyance/terraform-provider-relyance/internal/client"
	"github.com/relyance/terraform-provider-relyance/internal/common"
)

const (
	// defaultAudience is api://identity: tenant OAuth clients hold only the
	// generic tenant audience; the API gateway's token broker exchanges the
	// token for the target service's audience per route (x-service-audience).
	defaultAudience = "api://identity"
	// defaultEndpoint is the production API host; overridable via the
	// endpoint attribute or RELYANCE_ENDPOINT for non-prod environments.
	defaultEndpoint = "https://beta.api.relyance.ai"
	// defaultTimeoutSec is the HTTP client timeout when unset or set to a
	// non-positive value.
	defaultTimeoutSec = 60
)

// Bundle is what resources/data sources receive via Configure. Resources
// type-assert the interface below rather than this concrete type.
type Bundle struct {
	Client         *client.Client
	ValidateOnPlan bool
}

// GetClient satisfies the resource-side type assertion.
func (b *Bundle) GetClient() *client.Client { return b.Client }

// GetValidateOnPlan reports whether plan-time server validation is enabled.
func (b *Bundle) GetValidateOnPlan() bool { return b.ValidateOnPlan }

func configureProvider(ctx context.Context, req tfprovider.ConfigureRequest, resp *tfprovider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := firstNonEmpty(strVal(cfg.Endpoint), firstNonEmpty(os.Getenv("RELYANCE_ENDPOINT"), defaultEndpoint))
	endpoint = strings.TrimRight(endpoint, "/")
	// Credentials (client_secret / signed assertion) are transmitted to endpoint's
	// /oauth/token, so refuse to send them over cleartext http (loopback allowed for testing).
	if err := requireSecureEndpoint(endpoint); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Insecure endpoint", err.Error())
		return
	}
	clientID := firstNonEmpty(strVal(cfg.ClientID), os.Getenv("RELYANCE_CLIENT_ID"))
	clientSecret := firstNonEmpty(strVal(cfg.ClientSecret), os.Getenv("RELYANCE_CLIENT_SECRET"))
	jwkJSON := firstNonEmpty(strVal(cfg.JWKJSON), os.Getenv("RELYANCE_JWK_JSON"))
	audience := firstNonEmpty(strVal(cfg.Audience), firstNonEmpty(os.Getenv("RELYANCE_AUDIENCE"), defaultAudience))
	tokenURL := endpoint + "/oauth/token"

	scopes, diags := readStringList(ctx, cfg.Scopes)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout := resolveTimeout(cfg)
	retryMax := intVal(cfg.RetryMax, 3, "RELYANCE_RETRY_MAX")
	waitMin := durSec(intVal(cfg.RetryWaitMinSec, 1, ""))
	waitMax := durSec(intVal(cfg.RetryWaitMaxSec, 30, ""))
	ua := buildUA("terraform-provider-relyance/0.1.0", strVal(cfg.UserAgentSuffix))

	authCfg := auth.Config{
		TokenURL:     tokenURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		JWKJSON:      jwkJSON,
		Audience:     audience,
		Scopes:       scopes,
	}
	ts, err := auth.NewTokenSource(authCfg, &http.Client{Timeout: timeout})
	if err != nil {
		resp.Diagnostics.AddError("Provider authentication misconfigured", err.Error())
		return
	}

	rt := http.DefaultTransport
	rt = &oauth2.Transport{Base: rt, Source: ts}
	rt = common.RetryRoundTripper{Base: rt, Max: retryMax, WaitMin: waitMin, WaitMax: waitMax, RespectHeaders: true}
	rt = common.UserAgentRoundTripper{Base: rt, UA: ua}
	httpClient := &http.Client{Transport: rt, Timeout: timeout}

	validateOnPlan := true
	if !cfg.ValidateOnPlan.IsNull() && !cfg.ValidateOnPlan.IsUnknown() {
		validateOnPlan = cfg.ValidateOnPlan.ValueBool()
	}

	tflog.Debug(ctx, "relyance provider configured", map[string]any{
		"endpoint": endpoint,
		"audience": audience,
	})

	bundle := &Bundle{Client: client.New(endpoint, httpClient), ValidateOnPlan: validateOnPlan}
	resp.DataSourceData = bundle
	resp.ResourceData = bundle
}

func readStringList(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	var vals []types.String
	diags := list.ElementsAs(ctx, &vals, false)
	if diags.HasError() {
		return nil, diags
	}
	var out []string
	for _, v := range vals {
		if v.IsNull() || v.IsUnknown() {
			continue
		}
		if trimmed := strings.TrimSpace(v.ValueString()); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, diags
}

func strVal(s types.String) string {
	if s.IsNull() || s.IsUnknown() {
		return ""
	}
	return s.ValueString()
}

func intVal(v types.Int64, def int64, env string) int64 {
	if !v.IsNull() && !v.IsUnknown() {
		return v.ValueInt64()
	}
	if env != "" {
		if ev := os.Getenv(env); ev != "" {
			if n, err := strconv.ParseInt(ev, 10, 64); err == nil {
				return n
			}
		}
	}
	return def
}

// requireSecureEndpoint rejects any endpoint whose credentials would transit
// cleartext. https is always allowed; http only for loopback hosts (local mocks/tests).
func requireSecureEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("endpoint %q is not a valid URL: %w", endpoint, err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("endpoint must use https (got %q): OAuth credentials are sent to it", endpoint)
	default:
		return fmt.Errorf("endpoint must use https (got scheme %q in %q)", u.Scheme, endpoint)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// resolveTimeout resolves the HTTP client timeout, clamping a non-positive
// value (an explicit timeout_seconds = 0 or negative) to the default. Left
// unclamped, http.Client{Timeout: 0} means "no timeout", so a stray 0 would
// let a hung server hang the whole apply indefinitely.
func resolveTimeout(cfg providerModel) time.Duration {
	secs := intVal(cfg.TimeoutSec, defaultTimeoutSec, "RELYANCE_TIMEOUT_SECONDS")
	if secs <= 0 {
		secs = defaultTimeoutSec
	}
	return time.Duration(secs) * time.Second
}

func durSec(n int64) time.Duration {
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func buildUA(base, suffix string) string {
	if strings.TrimSpace(suffix) == "" {
		return base
	}
	return base + " " + strings.TrimSpace(suffix)
}
