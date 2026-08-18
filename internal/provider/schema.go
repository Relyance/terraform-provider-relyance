// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	pschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type providerModel struct {
	Endpoint       types.String `tfsdk:"endpoint"`
	ClientID       types.String `tfsdk:"client_id"`
	ClientSecret   types.String `tfsdk:"client_secret"`
	JWKJSON        types.String `tfsdk:"jwk_json"`
	Audience       types.String `tfsdk:"audience"`
	Scopes         types.List   `tfsdk:"scopes"`
	ValidateOnPlan types.Bool   `tfsdk:"validate_on_plan"`

	TimeoutSec      types.Int64  `tfsdk:"timeout_seconds"`
	RetryMax        types.Int64  `tfsdk:"retry_max"`
	RetryWaitMinSec types.Int64  `tfsdk:"retry_wait_min_seconds"`
	RetryWaitMaxSec types.Int64  `tfsdk:"retry_wait_max_seconds"`
	UserAgentSuffix types.String `tfsdk:"user_agent_suffix"`
}

func providerSchema() pschema.Schema {
	return pschema.Schema{
		Description: "Manages Relyance tenant configuration. v1 covers integration connections " +
			"(the integrations-api surface). Authenticates as a tenant OAuth client via " +
			"client_credentials with either a client secret or a private_key_jwt assertion.",
		Attributes: map[string]pschema.Attribute{
			"endpoint": pschema.StringAttribute{
				Optional: true,
				Description: "Relyance API host. Defaults to the production endpoint; override only for " +
					"non-production environments. Env: RELYANCE_ENDPOINT",
			},
			"client_id": pschema.StringAttribute{
				Optional:    true,
				Description: "Tenant OAuth client id. Env: RELYANCE_CLIENT_ID",
			},
			"client_secret": pschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "OAuth client secret (client_secret_post). Conflicts with jwk_json. Env: RELYANCE_CLIENT_SECRET",
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("jwk_json")),
				},
			},
			"jwk_json": pschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Private JWK (JSON) for private_key_jwt client authentication. Conflicts with client_secret. Env: RELYANCE_JWK_JSON",
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("client_secret")),
				},
			},
			"audience": pschema.StringAttribute{
				Optional:    true,
				Description: "Token audience. Default api://identity — the edge gateway exchanges per-route; override only for testing. Env: RELYANCE_AUDIENCE",
			},
			"scopes": pschema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Scope slugs to request on the token. Usually unnecessary: authorization is permission-based server-side.",
			},
			"validate_on_plan": pschema.BoolAttribute{
				Optional: true,
				Description: "Call the side-effect-free server validation endpoint during plan when auth values are fully known " +
					"(default true). Validation errors then surface at plan time instead of apply time.",
			},
			"timeout_seconds": pschema.Int64Attribute{
				Optional: true,
				Description: "HTTP client timeout in seconds (default 60). A non-positive value is clamped " +
					"to the default — the timeout cannot be disabled. Env: RELYANCE_TIMEOUT_SECONDS",
			},
			"retry_max": pschema.Int64Attribute{
				Optional:    true,
				Description: "Max retries for 429/5xx (default 3). Env: RELYANCE_RETRY_MAX",
			},
			"retry_wait_min_seconds": pschema.Int64Attribute{
				Optional:    true,
				Description: "Minimum retry backoff (default 1).",
			},
			"retry_wait_max_seconds": pschema.Int64Attribute{
				Optional:    true,
				Description: "Maximum retry backoff (default 30).",
			},
			"user_agent_suffix": pschema.StringAttribute{
				Optional:    true,
				Description: "Appended to the User-Agent header.",
			},
		},
	}
}
