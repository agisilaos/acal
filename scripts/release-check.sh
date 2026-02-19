#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ "$(uname -s)" != "Darwin" ]]; then
  die "release-check.sh must be run on macOS (Darwin)"
fi

if [[ $# -ne 1 ]]; then
  echo "usage: scripts/release-check.sh vX.Y.Z" >&2
  exit 2
fi

version="$1"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  die "version must match vX.Y.Z (got: $version)"
fi

for tool in go git python3; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "not inside a git work tree"
git diff --quiet || die "working tree has unstaged changes"
git diff --cached --quiet || die "index has staged changes"

if git rev-parse "$version" >/dev/null 2>&1; then
  die "tag already exists: $version"
fi

[[ -f README.md ]] || die "README.md not found"
[[ -f CHANGELOG.md ]] || die "CHANGELOG.md not found"

if rg -q "^## \[$version\]" CHANGELOG.md; then
  die "CHANGELOG.md already contains $version"
fi

echo "[release-check] running tests"
go test ./...

echo "[release-check] running vet"
go vet ./...

echo "[release-check] running docs check"
./scripts/docs-check.sh

echo "[release-check] checking format"
if [[ -n "$(gofmt -l cmd internal)" ]]; then
  die "gofmt reported formatting drift in cmd/ or internal/"
fi

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
  die "version output mismatch: $version_out"
fi

echo "[release-check] ok"
echo "  version:   $version"
echo "  commit:    $commit"
echo "  buildDate: $build_date"
echo "  binary:    $out_bin"
