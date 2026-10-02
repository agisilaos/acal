#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
source ./scripts/release-config.sh
source ./scripts/cli-shared/release-core.sh
cli_tooling_check "$CLI_TEMPLATE_FINGERPRINT"

# Check metadata before Go commands can repair a missing checksum.
echo "[verify] checking module metadata"
./scripts/cli-shared/module-check.sh

# Keep the original release portability gate for stock macOS runner tools.
if grep -R -nE '(^|[[:space:]])(r[g]|j[q]|y[q]|f[d])([[:space:]]|$)' scripts >/dev/null; then
  cli_release_die "scripts/ uses non-portable tooling (rg/jq/yq/fd). Use grep/sed/awk or install tools explicitly in workflow."
fi

echo "[verify] checking format"
unformatted="$(gofmt -l cmd internal tools)"
[[ -z "$unformatted" ]] || cli_release_die "Go files need formatting: $unformatted"
echo "[verify] running vet"
go vet ./...
echo "[verify] running tests"
go test ./...
echo "[verify] checking docs and help"
./scripts/docs-check.sh
echo "[verify] testing native proof without Calendar access"
make native-proof-test
echo "[verify] passed"
