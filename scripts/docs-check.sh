#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: docs-check.sh must be run on macOS (Darwin)" >&2
  exit 1
fi

if [[ ! -f README.md ]]; then
  echo "error: README.md not found" >&2
  exit 1
fi

if [[ ! -f CHANGELOG.md ]]; then
  echo "error: CHANGELOG.md not found" >&2
  exit 1
fi

for file in RELEASING.md scripts/changelog-context.sh scripts/changelog-section.py; do
  if [[ ! -f "$file" ]]; then
    echo "error: $file not found" >&2
    exit 1
  fi
done

for target in changelog-context release-check release-check-ci release-dry-run release; do
  if ! grep -qE "^${target}:" Makefile; then
    echo "error: Makefile missing target: $target" >&2
    exit 1
  fi
done

echo "[docs-check] validating shared docs contract"
python3 ./scripts/docs-contract-check.py

echo "[docs-check] checking CLI help snapshots"
./scripts/check-help.sh

echo "[docs-check] validating README command examples against the CLI command inventory"
python3 ./scripts/readme-command-check.py
python3 -B ./scripts/test-readme-command-check.py

echo "[docs-check] checking roadmap completion markers"
if [[ -f docs/cli-expansion-roadmap.md ]]; then
  if grep -nE -- '- \[ \] Step ' docs/cli-expansion-roadmap.md >/dev/null; then
    echo "error: docs/cli-expansion-roadmap.md still has unchecked steps" >&2
    exit 1
  fi
fi

echo "[docs-check] checking release script references in README"
if ! grep -Fq 'scripts/release-check.sh' README.md; then
  echo "error: README missing scripts/release-check.sh reference" >&2
  exit 1
fi
if ! grep -Fq 'scripts/release.sh' README.md; then
  echo "error: README missing scripts/release.sh reference" >&2
  exit 1
fi
if ! grep -Fq 'make release-check VERSION=vX.Y.Z' README.md; then
  echo "error: README missing make release-check usage" >&2
  exit 1
fi
if ! grep -Fq 'make release VERSION=vX.Y.Z' README.md; then
  echo "error: README missing make release usage" >&2
  exit 1
fi
if ! grep -Fq 'make release-dry-run VERSION=vX.Y.Z' README.md; then
  echo "error: README missing make release-dry-run usage" >&2
  exit 1
fi

echo "[docs-check] ok"
