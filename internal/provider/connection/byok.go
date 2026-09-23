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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/relyance/terraform-provider-relyance/internal/client"
)

// InHost BYOK ("bring your own key") connections keep every credential in the
// customer's environment. With secret_ref, the customer stores all credential
// fields of the auth method as one JSON object in AWS Secrets Manager in its
// own AWS account, and Relyance stores only the secret's ARN. Only top-level
// auth fields (for example data_storage_location) are still sent to Relyance.

// secretRefPattern is the server's accepted AWS Secrets Manager secret ARN
// shape (aws, aws-us-gov and aws-cn partitions). Group 1 is the partition,
// group 2 the region.
var secretRefPattern = regexp.MustCompile(
	`^arn:(aws|aws-us-gov|aws-cn):secretsmanager:([a-z0-9-]+):\d{12}:secret:[A-Za-z0-9/_+=.@-]+$`)

const secretRefPatternMessage = "must be an AWS Secrets Manager secret ARN, " +
	"e.g. arn:aws:secretsmanager:us-east-1:123456789012:secret:relyance/inhost/jira-AbCdEf"

// secretRefProblem returns why v is not an acceptable secret_ref ("" if it
// is). It mirrors the server: the ARN shape, and a region that belongs to the
// ARN's partition (aws-cn: cn-*, aws-us-gov: us-gov-*, aws: neither).
func secretRefProblem(v string) string {
	m := secretRefPattern.FindStringSubmatch(v)
	if m == nil {
		return secretRefPatternMessage
	}
	partition, region := m[1], m[2]
	var ok bool
	switch partition {
	case "aws-cn":
		ok = strings.HasPrefix(region, "cn-")
	case "aws-us-gov":
		ok = strings.HasPrefix(region, "us-gov-")
	default:
		ok = !strings.HasPrefix(region, "cn-") && !strings.HasPrefix(region, "us-gov-")
	}
	if !ok {
		return fmt.Sprintf("The region %s is not in the AWS partition %s.", region, partition)
	}
	return ""
}

// secretRefValidator is the plan-time secret_ref check (secretRefProblem).
type secretRefValidator struct{}

func (secretRefValidator) Description(context.Context) string {
	return "value must be an AWS Secrets Manager secret ARN whose region is in the ARN's partition"
}

func (v secretRefValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (secretRefValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !knownString(req.ConfigValue) {
		return
	}
	if problem := secretRefProblem(req.ConfigValue.ValueString()); problem != "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid secret_ref",
			fmt.Sprintf("%q: %s", req.ConfigValue.ValueString(), problem))
	}
}

// byokCredentialsHint tells the practitioner where BYOK credentials go.
const byokCredentialsHint = "Relyance never receives the credentials of an InHost BYOK connection. " +
	"Put every credential field of the auth method (secret and non-secret) in the JSON object of the " +
	"AWS Secrets Manager secret that secret_ref points to (or in the Kubernetes secret of your InHost " +
	"deployment). Only top-level fields, such as data_storage_location, go in auth.params."

// targetRuntimeMode is the runtime_mode the connection will have after the
// apply: the configured value; else the current one (runtime_mode is not
// managed when unset); else, on create, the server default. known is false
// when the configured value is not known yet.
func targetRuntimeMode(cfgMode types.String, state *resourceModel) (mode string, known bool) {
	switch {
	case cfgMode.IsUnknown():
		return "", false
	case !cfgMode.IsNull():
		return cfgMode.ValueString(), true
	case state == nil:
		return client.RuntimeModeRelyanceHosted, true
	case knownString(state.RuntimeMode):
		return state.RuntimeMode.ValueString(), true
	default:
		return "", false
	}
}

func knownString(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }

// validateSecretRefConfig holds the config-only secret_ref rules (no state, no
// network): a reference needs an auth method to attach to, and it cannot be
// combined with secret values, which a BYOK connection never accepts.
func validateSecretRefConfig(ctx context.Context, cfg *resourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if !knownString(cfg.SecretRef) {
		return diags
	}
	if knownString(cfg.RuntimeMode) && cfg.RuntimeMode.ValueString() != client.RuntimeModeInHostBYOK {
		diags.AddAttributeError(path.Root("secret_ref"), "secret_ref needs runtime_mode IN_HOST_BYOK",
			fmt.Sprintf("secret_ref is only for InHost BYOK connections, but runtime_mode is %q. "+
				"Set runtime_mode = %q, or remove secret_ref.", cfg.RuntimeMode.ValueString(), client.RuntimeModeInHostBYOK))
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

// byokPlanDiags holds the plan-time rules that need no network. mode/known
// is targetRuntimeMode. cfgSecrets is auth.secrets_wo from CONFIG (write-only
// values are never in the plan).
func byokPlanDiags(ctx context.Context, plan *resourceModel, mode string, known bool, cfgSecrets types.Map) diag.Diagnostics {
	var diags diag.Diagnostics
	if !known {
		return diags
	}
	// Unknown counts as set: the ARN of a secret created in the same apply is
	// unknown at plan time, and it must not slip past this check.
	if !plan.SecretRef.IsNull() && mode != client.RuntimeModeInHostBYOK {
		diags.AddAttributeError(path.Root("secret_ref"), "secret_ref needs runtime_mode IN_HOST_BYOK",
			fmt.Sprintf("secret_ref is only for InHost BYOK connections, but this connection's runtime_mode "+
				"will be %q. Set runtime_mode = %q, or remove secret_ref.", mode, client.RuntimeModeInHostBYOK))
	}
	if mode == client.RuntimeModeInHostBYOK && plan.Auth != nil {
		if keys := mapKeys(ctx, cfgSecrets, &diags); len(keys) > 0 {
			diags.AddAttributeError(path.Root("auth").AtName("secrets_wo"), "Credentials on an InHost BYOK connection",
				fmt.Sprintf("auth.secrets_wo sets %s. %s Remove auth.secrets_wo.", quoteJoin(keys), byokCredentialsHint))
		}
	}
	return diags
}

// leavingBYOK reports a switch of an existing InHost BYOK connection to
// another runtime mode. state is the prior state (nil on create); mode/known
// is targetRuntimeMode.
func leavingBYOK(state *resourceModel, mode string, known bool) bool {
	return state != nil && known && mode != client.RuntimeModeInHostBYOK &&
		knownString(state.RuntimeMode) && state.RuntimeMode.ValueString() == client.RuntimeModeInHostBYOK
}

// modeSwitchCredentialDiags: Relyance holds no credentials for an InHost BYOK
// connection, so a switch to another runtime mode must send them (the apply
// saves auth after the switch). Unknown auth.secrets_wo is left to the apply.
func modeSwitchCredentialDiags(ctx context.Context, cfg *resourceModel, state *resourceModel, mode string, known bool) diag.Diagnostics {
	var diags diag.Diagnostics
	if !leavingBYOK(state, mode, known) {
		return diags
	}
	if cfg.Auth != nil {
		if cfg.Auth.SecretsWO.IsUnknown() || len(mapKeys(ctx, cfg.Auth.SecretsWO, &diags)) > 0 {
			return diags
		}
	}
	diags.AddAttributeError(path.Root("auth").AtName("secrets_wo"), "Credentials needed to leave InHost BYOK",
		fmt.Sprintf("This connection is InHost BYOK, so Relyance holds no credentials for it. runtime_mode %q "+
			"needs them: set auth.method, the secret fields in auth.secrets_wo and the other fields in "+
			"auth.params. The apply saves them after the runtime_mode change.", mode))
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
// the stored reference unchanged, "" clears it, anything else sets it. mode is
// the runtime_mode after the apply: off BYOK nothing is sent, because the
// server clears secret_ref itself when the mode switches away from BYOK (and
// rejects secretRef on any other mode).
func secretRefWire(plan types.String, state *resourceModel, mode string) *string {
	if plan.IsUnknown() || mode != client.RuntimeModeInHostBYOK {
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
