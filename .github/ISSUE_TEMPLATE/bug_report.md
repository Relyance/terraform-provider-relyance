---
name: Bug report
about: Report a problem with the Relyance Terraform provider
title: "[Bug]: "
labels: ["bug"]
---

<!--
Thank you for taking the time to file a bug report.

IMPORTANT: Never paste real secrets or credentials into this issue — that includes
client secrets, JWKs/private keys, connection auth values, tokens, or tenant identifiers
you consider sensitive. Redact them (e.g. `client_secret = "REDACTED"`) in any config or
log output below.
-->

### Provider version

<!-- e.g. 0.1.0 -->

### Terraform version

<!-- output of `terraform version` -->

### Affected resource(s) or data source(s)

<!-- e.g. relyance_integration_connection, relyance_integration_vendor -->

### Terraform configuration

<!-- Minimal config that reproduces the issue. Redact secrets/tenant-specific values. -->

```hcl

```

### Expected behavior

<!-- What you expected to happen. -->

### Actual behavior

<!-- What actually happened. Include the exact error message if any. -->

### Steps to reproduce

1.
2.
3.

### Debug output

<!--
Re-run with logging enabled and paste the relevant excerpt (redact secrets/tokens first):

  export TF_LOG=DEBUG
  terraform apply

Consider a link to a full log via a Gist if it's long, rather than pasting everything here.
-->

```
```

### Additional context

<!-- Anything else that might help — related issues, environment specifics, etc. -->
