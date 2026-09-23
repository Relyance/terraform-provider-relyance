// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package connection

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

// InHost BYOK ("bring your own key") connections keep every credential in the
// customer's environment. With secret_ref, the customer stores all credential
// fields of the auth method as one JSON object in AWS Secrets Manager in its
// own AWS account, and Relyance stores only the secret's ARN. Only top-level
// auth fields (for example data_storage_location) are still sent to Relyance.

// secretRefPattern is the server's accepted AWS Secrets Manager secret ARN
// shape (aws, aws-us-gov and aws-cn partitions).
var secretRefPattern = regexp.MustCompile(
	`^arn:aws(-us-gov|-cn)?:secretsmanager:[a-z0-9-]+:\d{12}:secret:[A-Za-z0-9/_+=.@-]+$`)

const secretRefPatternMessage = "must be an AWS Secrets Manager secret ARN, " +
	"e.g. arn:aws:secretsmanager:us-east-1:123456789012:secret:relyance/inhost/jira-AbCdEf"

// byokCredentialsHint tells the practitioner where BYOK credentials go.
const byokCredentialsHint = "Relyance never receives the credentials of an InHost BYOK connection. " +
	"Put every credential field of the auth method (secret and non-secret) in the JSON object of the " +
	"AWS Secrets Manager secret that secret_ref points to (or in the Kubernetes secret of your InHost " +
	"deployment). Only top-level fields, such as data_storage_location, go in auth.params."

func knownString(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }

// isBYOK reports whether the prior state says the connection runs as InHost
// BYOK. Terraform refreshes state before planning, so this is the server's
// current runtime_mode (runtime_mode is set in the Relyance app, not here).
func isBYOK(state *resourceModel) bool {
	return state != nil && knownString(state.RuntimeMode) &&
		state.RuntimeMode.ValueString() == client.RuntimeModeInHostBYOK
}

// validateSecretRefConfig holds the config-only secret_ref rules (no state, no
// network): a reference needs an auth method to attach to, and it cannot be
// combined with secret values, which a BYOK connection never accepts.
func validateSecretRefConfig(ctx context.Context, cfg *resourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if !knownString(cfg.SecretRef) {
		return diags
	}
	if cfg.Auth == nil {
		diags.AddAttributeError(path.Root("secret_ref"), "secret_ref needs an auth block",
			"Set auth.method to the authentication method whose credential fields the secret holds "+
				"(see the relyance_integration_vendor data source).")
		return diags
	}
	if keys := mapKeys(ctx, cfg.Auth.SecretsWO, &diags); len(keys) > 0 {
		diags.AddAttributeError(path.Root("auth").AtName("secrets_wo"), "Credentials set together with secret_ref",
			fmt.Sprintf("auth.secrets_wo sets %s, but secret_ref is set. %s Remove auth.secrets_wo.",
				quoteJoin(keys), byokCredentialsHint))
	}
	return diags
}

// byokPlanDiags holds the plan-time rules that need the prior state but no
// network. state is nil on create. cfgSecrets is auth.secrets_wo from CONFIG
// (write-only values are never in the plan).
func byokPlanDiags(ctx context.Context, plan, state *resourceModel, cfgSecrets types.Map) diag.Diagnostics {
	var diags diag.Diagnostics

	// Unknown counts as set: the ARN of a secret created in the same apply is
	// unknown at plan time, and it must not slip past these checks.
	if !plan.SecretRef.IsNull() {
		switch {
		case state == nil:
			diags.AddAttributeError(path.Root("secret_ref"), "secret_ref needs an InHost BYOK connection",
				"secret_ref can only be set on a connection whose runtime_mode is IN_HOST_BYOK. A new "+
					"connection starts as RELYANCE_HOSTED, and the runtime mode is set in the Relyance app. "+
					"Create the connection without secret_ref, switch it to InHost BYOK in the Relyance app, "+
					"then add secret_ref. Or import an existing InHost BYOK connection.")
		case knownString(state.RuntimeMode) && !isBYOK(state):
			diags.AddAttributeError(path.Root("secret_ref"), "secret_ref needs an InHost BYOK connection",
				fmt.Sprintf("secret_ref requires runtime_mode = %q, but this connection's runtime_mode is %q. "+
					"Switch the connection to InHost BYOK in the Relyance app, or remove secret_ref.",
					client.RuntimeModeInHostBYOK, state.RuntimeMode.ValueString()))
		}
	}

	if isBYOK(state) && plan.Auth != nil {
		if keys := mapKeys(ctx, cfgSecrets, &diags); len(keys) > 0 {
			diags.AddAttributeError(path.Root("auth").AtName("secrets_wo"), "Credentials on an InHost BYOK connection",
				fmt.Sprintf("auth.secrets_wo sets %s. %s Remove auth.secrets_wo.", quoteJoin(keys), byokCredentialsHint))
		}
	}
	return diags
}

// byokParamDiags rejects auth.params keys that are not top-level fields of the
// matched auth method: on a BYOK connection those are credentials, and they
// belong in the customer's secret. Needs the catalog (the top-level flags).
func byokParamDiags(params map[string]string, matched *client.AuthConfig, vendorKey string) diag.Diagnostics {
	var diags diag.Diagnostics
	topLevel := map[string]bool{}
	for _, f := range matched.CustomFields {
		topLevel[f.Key] = f.TopLevel()
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		isTop, known := topLevel[k]
		if !known || isTop {
			continue // unknown keys are reported by the generic check
		}
		diags.AddAttributeError(path.Root("auth").AtName("params"), "Credential field in auth.params on an InHost BYOK connection",
			fmt.Sprintf("field %q of method %s for vendor %s is not a top-level field. %s Move %q to the secret.",
				k, matched.Slug, vendorKey, byokCredentialsHint, k))
	}
	return diags
}

// secretRefWire is the secret_ref value to send on an auth save: nil leaves
// the stored reference unchanged, "" clears it, anything else sets it.
func secretRefWire(plan types.String, state *resourceModel) *string {
	if plan.IsUnknown() {
		return nil
	}
	prior := types.StringNull()
	if state != nil {
		prior = state.SecretRef
	}
	if plan.IsNull() {
		if knownString(prior) {
			empty := ""
			return &empty // removed from config: clear it server-side
		}
		return nil
	}
	if plan.Equal(prior) {
		return nil
	}
	v := plan.ValueString()
	return &v
}

// mapKeys returns the sorted keys of a known string map (nil when null/unknown).
func mapKeys(ctx context.Context, m types.Map, diags *diag.Diagnostics) []string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	var vals map[string]string
	diags.Append(m.ElementsAs(ctx, &vals, false)...)
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func quoteJoin(keys []string) string {
	q := make([]string, len(keys))
	for i, k := range keys {
		q[i] = fmt.Sprintf("%q", k)
	}
	return strings.Join(q, ", ")
}
