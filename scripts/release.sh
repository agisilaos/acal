#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
source ./scripts/release-config.sh
export GOTOOLCHAIN="$RELEASE_GO_TOOLCHAIN"
source ./scripts/cli-shared/release-core.sh
cli_tooling_check "$CLI_TEMPLATE_FINGERPRINT"
source ./scripts/cli-shared/release.sh "$@"
