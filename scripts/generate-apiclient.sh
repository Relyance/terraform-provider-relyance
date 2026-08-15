#!/usr/bin/env bash
# Regenerate the vendored integrations-api Go client (internal/apiclient) from
# the curated OpenAPI spec at internal/apiclient/openapi.json.
#
# The spec is the `terraform` profile slice of integrations-api's OpenAPI — only
# the provider-facing endpoints (connections/v1, catalog/v1 vendors, the
# classification business-nodes lookup), not the mostly-internal rest of the
# surface. To refresh the spec itself, re-export it from integrations-api-client
# (the `terraform` openapi-format profile) and drop it in place, then run this.
#
# The committed output is drift-checked in CI, so a stale client fails the build.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT_DIR/internal/apiclient"
# openapi-generator v7.11.0, pinned by digest (this repo SHA-pins actions/images).
GEN_IMAGE="${OPENAPI_GENERATOR_IMAGE:-openapitools/openapi-generator-cli@sha256:a9e7091ac8808c6835cf8ec88252bca603f1f889ef1456b63d8add5781feeca7}"

# disallowAdditionalPropertiesIfNotPresent=false: a schema with no
#   additionalProperties key is OPEN (matches JSON Schema), so response models
#   keep an AdditionalProperties catch-all instead of DisallowUnknownFields --
#   an additive server field can't hard-fail decode in already-shipped clients.
# enumUnknownDefaultCase=true: response enums tolerate an unknown value (map it
#   to a sentinel) instead of failing the whole payload -- same forward-compat
#   concern given the provider's release cadence is decoupled from the server.
docker run --rm --user "$(id -u):$(id -g)" -v "$ROOT_DIR":/local \
  "${GEN_IMAGE}" generate \
  -i /local/internal/apiclient/openapi.json -g go -o /local/internal/apiclient \
  --package-name apiclient \
  --additional-properties=isGoSubmodule=true,enumClassPrefix=true,withGoMod=false,disallowAdditionalPropertiesIfNotPresent=false,enumUnknownDefaultCase=true \
  --global-property=apiTests=false,modelTests=false

# Drop generator boilerplate we don't vendor (README/donation text, per-model
# markdown, git_push.sh, CI configs). Keep only Go sources + the spec.
rm -rf \
  "$OUT/README.md" "$OUT/docs" "$OUT/api" "$OUT/git_push.sh" \
  "$OUT/.travis.yml" "$OUT/.gitignore" \
  "$OUT/.openapi-generator" "$OUT/.openapi-generator-ignore"

gofmt -w "$OUT"

echo "[generate-apiclient] regenerated $OUT from openapi.json"
