#!/usr/bin/env bash
set -euo pipefail
umask 077

# Build the production entrypoint without regeneration, module download, release,
# application boot or the previous green test groups. Tests use only local fake
# HTTP and private config/cache directories with the user's D-Bus disconnected.
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cli_dir=$(cd -- "$script_dir/.." && pwd)
development_dir=$(cd -- "$cli_dir/.." && pwd)
toolchain_dir="$development_dir/toolchains/go1.26.4"
batch82_dir="$toolchain_dir/installed-batch82"
report_dir="$development_dir/runtime-evidence/resume-20261010"

mkdir -p -- "$batch82_dir" "$report_dir"
chmod 700 "$batch82_dir"
export GOTOOLCHAIN=local
export GOMODCACHE="$toolchain_dir/mod-cache"
export GOCACHE="$toolchain_dir/build-cache"
export GOPROXY=off
export GOSUMDB=off
export GOFLAGS=-mod=readonly
export FACTUAREA_CRM_INSTALLED_BIN="$batch82_dir/factuarea"

cd -- "$cli_dir"
"$toolchain_dir/go/bin/go" build -trimpath -o "$FACTUAREA_CRM_INSTALLED_BIN" ./cmd/factuarea
chmod 700 "$FACTUAREA_CRM_INSTALLED_BIN"
sha256sum "$FACTUAREA_CRM_INSTALLED_BIN" > "$report_dir/cli-crm-installed-batch82-binary.sha256"
"$toolchain_dir/go/bin/go" test ./internal/cmd -run '^TestCRMInstalledBatch82$' -count=1 -json \
  > "$report_dir/cli-crm-installed-batch82-go.jsonl"
