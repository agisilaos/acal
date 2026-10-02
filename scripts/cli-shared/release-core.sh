#!/usr/bin/env bash
# Sourced by the repository-owned release-check entry point, from its root.

cli_release_die() {
  echo "error: $*" >&2
  exit 1
}

cli_tooling_check() {
  python3 - "${1:-}" <<'PY'
import hashlib
import json
from pathlib import Path
import re
import stat
import sys

try:
    root = Path('scripts/cli-shared')
    manifest = Path('scripts/cli-tooling-manifest.json')
    if Path('scripts').is_symlink() or root.is_symlink() or not root.is_dir() or manifest.is_symlink() or not manifest.is_file():
        raise ValueError('helpers and manifest must be regular local files')
    if stat.S_IMODE(manifest.stat().st_mode) != 0o644:
        raise ValueError('manifest mode must be 0644')
    document = json.loads(manifest.read_text())
    if not isinstance(document, dict) or set(document) != {'schema_version', 'template_revision', 'source_fingerprint', 'files'} or type(document['schema_version']) is not int or document['schema_version'] != 1:
        raise ValueError('unsupported manifest shape/version')
    if not isinstance(document['template_revision'], str) or not re.fullmatch(r'[A-Za-z0-9._:-]{1,128}', document['template_revision']):
        raise ValueError('invalid template revision')
    names = ['changelog-context.sh', 'changelog-section.py', 'docs-contract-check.py', 'module-check.sh', 'release-core.sh', 'release.sh']
    expected = {'scripts/cli-shared/' + name for name in names}
    records = document['files']
    if not isinstance(records, dict) or set(records) != expected:
        raise ValueError('manifest has unexpected helper paths')
    fingerprint = hashlib.sha256(json.dumps(records, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    if document['source_fingerprint'] != fingerprint:
        raise ValueError('manifest fingerprint does not match its records')
    if sys.argv[1] and (not re.fullmatch(r'[0-9a-f]{64}', sys.argv[1]) or fingerprint != sys.argv[1]):
        raise ValueError('manifest differs from the repository-pinned template fingerprint')
    for name, record in records.items():
        if not isinstance(record, dict) or set(record) != {'sha256', 'mode'} or record['mode'] not in ('0644', '0755'):
            raise ValueError('invalid helper record: ' + name)
        path = Path(name)
        if path.is_symlink() or not path.is_file():
            raise ValueError('missing/non-regular helper: ' + name)
        if hashlib.sha256(path.read_bytes()).hexdigest() != record['sha256'] or f'{stat.S_IMODE(path.stat().st_mode):04o}' != record['mode']:
            raise ValueError('helper content or mode drift: ' + name)
except (OSError, ValueError, TypeError, KeyError) as exc:
    print('error: CLI tooling integrity: ' + str(exc), file=sys.stderr)
    raise SystemExit(1)
print('CLI tooling integrity passed')
PY
}

cli_release_preflight() {
  ci_mode=0
  version=""
  if [[ $# -eq 1 && "$1" == "--ci" ]]; then
    ci_mode=1
  elif [[ $# -eq 1 && "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    version="$1"
  else
    cli_release_die "usage: scripts/release-check.sh vX.Y.Z | --ci"
  fi

  [[ "$(uname -s)" == "Darwin" ]] || cli_release_die "release-check.sh must be run on macOS (Darwin)"
  local tool
  for tool in go git python3 make; do
    command -v "$tool" >/dev/null 2>&1 || cli_release_die "$tool is required"
  done
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || cli_release_die "not inside a git work tree"
  [[ -z "$(git status --porcelain --untracked-files=all)" ]] || cli_release_die "working tree is not clean (tracked, staged, or untracked changes present)"
  if [[ "$ci_mode" -eq 0 ]] && git rev-parse -q --verify "refs/tags/$version" >/dev/null 2>&1; then
    cli_release_die "tag already exists: $version"
  fi
  [[ -f README.md ]] || cli_release_die "README.md not found"
  if [[ "$ci_mode" -eq 1 ]]; then
    version="$(python3 ./scripts/changelog-section.py --latest)"
  fi
  local -a changelog_args
  changelog_args=(--version "$version" --validate)
  if [[ "$ci_mode" -eq 0 ]]; then
    changelog_args+=(--require-traceability)
  fi
  python3 ./scripts/changelog-section.py "${changelog_args[@]}"
}

cli_release_build_check() {
  local build_version="$1"
  local out_dir="${2:-dist/release-check}"
  local commit build_date ldflags out_bin version_out expected
  commit="$(git rev-parse --short=12 HEAD)"
  build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  out_bin="$out_dir/$CLI_NAME"
  mkdir -p "$out_dir"
  ldflags="${RELEASE_LDFLAGS_TEMPLATE//\{\{VERSION\}\}/$build_version}"
  ldflags="${ldflags//\{\{COMMIT\}\}/$commit}"
  ldflags="${ldflags//\{\{DATE\}\}/$build_date}"
  echo "[release-check] building version-stamped binary"
  CGO_ENABLED="${RELEASE_CGO_ENABLED:-0}" go build -trimpath -ldflags "$ldflags" -o "$out_bin" "$DEFAULT_BUILD_PKG"
  version_out="$("$out_bin" "$DEFAULT_HOMEBREW_TEST_ARG")"
  expected="$CLI_NAME {{VERSION}} ({{COMMIT}}) {{DATE}}"
  expected="${RELEASE_VERSION_TEMPLATE:-$expected}"
  expected="${expected//\{\{VERSION\}\}/$build_version}"
  expected="${expected//\{\{COMMIT\}\}/$commit}"
  expected="${expected//\{\{DATE\}\}/$build_date}"
  [[ "$version_out" == "$expected" ]] || cli_release_die "version output mismatch: $version_out (expected: $expected)"
  printf '[release-check] ok\n  version: %s\n  commit: %s\n  buildDate: %s\n  binary: %s\n' "$build_version" "$commit" "$build_date" "$out_bin"
}
