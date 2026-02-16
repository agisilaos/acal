#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: release-check.sh must be run on macOS (Darwin)" >&2
  exit 1
fi

if [[ $# -ne 1 ]]; then
  echo "usage: scripts/release-check.sh vX.Y.Z" >&2
  exit 2
fi

version="$1"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: version must match vX.Y.Z (got: $version)" >&2
  exit 2
fi

if [[ ! -f CHANGELOG.md ]]; then
  echo "error: CHANGELOG.md not found" >&2
  exit 1
fi

if rg -q "^## \[$version\]" CHANGELOG.md; then
  echo "error: CHANGELOG.md already contains $version" >&2
  exit 1
fi

echo "[release-check] running tests"
go test ./...

echo "[release-check] running vet"
go vet ./...

commit="$(git rev-parse --short=12 HEAD)"
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
out_dir="dist/release-check"
out_bin="$out_dir/acal"

mkdir -p "$out_dir"

echo "[release-check] building version-stamped binary"
go build \
  -ldflags "-X main.version=$version -X main.commit=$commit -X main.date=$build_date" \
  -o "$out_bin" \
  ./cmd/acal

version_out="$($out_bin version)"
if [[ "$version_out" != acal\ "$version"* ]]; then
  echo "error: version output mismatch: $version_out" >&2
  exit 1
fi

echo "[release-check] ok"
echo "  version:   $version"
echo "  commit:    $commit"
echo "  buildDate: $build_date"
echo "  binary:    $out_bin"
