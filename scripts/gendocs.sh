#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root_dir"

if ! command -v terraform >/dev/null 2>&1; then
  echo "Terraform CLI not found in PATH. tfplugindocs will attempt to download it, which may require network access." >&2
fi

# tfplugindocs lives in the separate tools/ module, so run it from there. --provider-dir is
# absolute; rendered-website-dir / examples-dir are resolved RELATIVE to --provider-dir (two
# levels deep), hence ../../ to reach the repo root.
echo "Generating provider documentation into ./docs ..."
cd "$root_dir/tools"
go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate \
  --provider-name relyance \
  --provider-dir "$root_dir/cmd/terraform-provider-relyance" \
  --rendered-website-dir ../../docs \
  --examples-dir ../../examples \
  --website-source-dir ../../templates
echo "Done. Review changes in the docs/ folder."
