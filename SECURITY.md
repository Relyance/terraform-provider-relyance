# Security

## Reporting a vulnerability

Please report suspected security issues privately to **security@relyance.ai**. Do not open
a public GitHub issue for security reports. We aim to acknowledge within 3 business days.

## Authentication model

The provider authenticates as a **tenant OAuth client** using the OAuth 2.0
`client_credentials` grant, in one of two modes:

- **client secret** (`client_secret` / `RELYANCE_CLIENT_SECRET`) — `client_secret_post`.
- **private_key_jwt** (`jwk_json` / `RELYANCE_JWK_JSON`) — the provider signs a short-lived
  client assertion locally; the private key never leaves the machine running Terraform.
  Prefer this mode where available.

Tokens are minted with audience `api://identity`; the Relyance API edge exchanges them
per-route for the target service audience. The provider never holds a service audience
itself. Grant the OAuth client the **least privilege** it needs — for connection management
that is `integrations.catalog.read` plus `integrations.connections.{read,write,delete}`.
Do not reuse a high-privilege client.

## Secret handling

- **Write-only credentials.** Connection secret fields (those marked `is_secret` in the
  `relyance_integration_vendor` data source — e.g. `external_id`, API keys) go in
  `auth.secrets_wo`, a **write-only** attribute (Terraform ≥ 1.11). Write-only values are
  **never persisted to state or plan files**. Non-secret fields go in `auth.params`.
- **Rotation.** Because write-only values are never stored, they cannot be diffed. Re-send a
  rotated secret by incrementing `auth.secrets_wo_version`; that bump is the only trigger that
  resends `auth.secrets_wo`. `auth.credentials_fingerprint` (computed) lets you observe that
  the stored credential changed without exposing its value.
- **Provider credentials** (`client_secret`, `jwk_json`) are sensitive provider config —
  supply them via environment variables (`RELYANCE_CLIENT_SECRET` / `RELYANCE_JWK_JSON`) or a
  secrets manager, never committed to VCS.

## Known limitations

- **Deleting a connection orphans its Secret Manager secret.** This is server-side behavior in
  integrations-api (the DELETE removes the connection document but not the backing secret); the
  provider cannot clean it up client-side. Track/rotate those secrets out of band.
- **Import** recovers a connection's identity and scalars only. Write-only credentials cannot
  be read back, so after importing a connection you must supply `auth` in configuration and
  apply to reconcile it.
