#!/usr/bin/env bash
# Sourced by a repository-owned publisher after its release-config.sh.
set -euo pipefail

release_error() { echo "error: $*" >&2; exit 1; }

release_usage() {
  cat <<USAGE
Usage: ./scripts/release.sh [--dry-run] vX.Y.Z

Environment:
  HOMEBREW_TAP_REPO      Tap owner/name (default: agisilaos/homebrew-tap)
  HOMEBREW_TAP_URL       Clone URL (default: HTTPS URL for HOMEBREW_TAP_REPO)
  HOMEBREW_TAP_BRANCH    Existing tap branch (default: main)
  HOMEBREW_FORMULA_PATH  Relative formula path (default: ${DEFAULT_FORMULA_PATH:-})
  GITHUB_REPO            Release owner/name (default: GitHub origin)
  HOMEBREW_DESC          Formula description
  HOMEBREW_LICENSE       Formula license
  HOMEBREW_TEST_ARG      Shell-style version arguments (default: ${DEFAULT_HOMEBREW_TEST_ARG:-})
  RELEASE_BUILD_PKG      Local Go package (default: ${DEFAULT_BUILD_PKG:-})
  RELEASE_LDFLAGS        Build flags; configured version/commit/date stamps required
USAGE
}

DRY_RUN=0
VERSION=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) release_usage; exit 0 ;;
    v*) [[ -z "$VERSION" ]] || release_error "version provided multiple times"; VERSION="$1"; shift ;;
    *) release_error "unknown argument: $1" ;;
  esac
done
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || release_error "version must match vX.Y.Z"

for setting in CLI_NAME FORMULA_NAME ARTIFACT_NAME DEFAULT_BRANCH DEFAULT_HOMEBREW_DESC \
  DEFAULT_HOMEBREW_LICENSE DEFAULT_HOMEBREW_TEST_ARG DEFAULT_FORMULA_PATH DEFAULT_BUILD_PKG \
  RELEASE_LDFLAGS_TEMPLATE RELEASE_CGO_ENABLED RELEASE_INCLUDE_LICENSE; do
  [[ -n "${!setting:-}" ]] || release_error "missing release setting: $setting"
done
for name in "$CLI_NAME" "$FORMULA_NAME" "$ARTIFACT_NAME"; do
  [[ "$name" =~ ^[a-z][a-z0-9]*([-_][a-z0-9]+)*$ ]] || release_error "invalid binary/formula/archive name: $name"
done
[[ "$RELEASE_CGO_ENABLED" == 0 || "$RELEASE_CGO_ENABLED" == 1 ]] || release_error "RELEASE_CGO_ENABLED must be 0 or 1"
[[ "$RELEASE_INCLUDE_LICENSE" == 0 || "$RELEASE_INCLUDE_LICENSE" == 1 ]] || release_error "RELEASE_INCLUDE_LICENSE must be 0 or 1"

for tool in go git gh tar python3 ruby; do
  command -v "$tool" >/dev/null 2>&1 || release_error "required command not found: $tool"
done
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || release_error "must run inside a git repository"
release_commit="$(git rev-parse --verify HEAD)"
git check-ref-format --branch "$DEFAULT_BRANCH" >/dev/null 2>&1 || release_error "invalid release branch: $DEFAULT_BRANCH"
current_branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
if [[ "$current_branch" != "$DEFAULT_BRANCH" ]]; then
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "warning: current branch is ${current_branch:-detached HEAD}, expected $DEFAULT_BRANCH" >&2
  else
    release_error "release must run from $DEFAULT_BRANCH (current: ${current_branch:-detached HEAD})"
  fi
fi
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || release_error "working tree is not clean (tracked, staged, or untracked changes present)"
if git rev-parse --quiet --verify "refs/tags/$VERSION" >/dev/null 2>&1; then
  release_error "tag $VERSION already exists"
fi

release_phase="preflight"
local_tag_status="not attempted"
tag_push_status="not attempted"
github_status="not attempted"
homebrew_status="not attempted"
publication_started=0
artifacts_ready=0
tap_prepared=0
tmp_dir=""
tap_dir=""
dist_dir="dist"

# Outcome reports describe this invocation; a failed command may have succeeded remotely.
finish_release() {
  local status=$?
  trap - EXIT
  trap '' HUP INT TERM
  set +e
  if [[ "$status" -ne 0 ]]; then
    {
      echo "release $VERSION stopped during: $release_phase (exit $status)"
      if [[ "$publication_started" -eq 1 ]]; then
        echo "Publication commands in this run:"
        echo "  Local tag: $local_tag_status"
        echo "  Tag push: $tag_push_status"
        echo "  GitHub release/assets: $github_status"
        echo "  Homebrew push: $homebrew_status"
        echo "Expected release commit: $release_commit"
        echo "Do not rerun release.sh or release-dry-run: rebuilding can change artifact checksums."
        echo "Inspect remote state before repeating any write; an error does not prove it failed remotely."
        echo "Run these read-only checks from this checkout:"
        printf '  git rev-parse %q\n' "refs/tags/$VERSION^{commit}"
        printf '  git ls-remote origin %q %q\n' "refs/tags/$VERSION" "refs/tags/$VERSION^{}"
        printf '  gh release view %q --repo %q --json url,assets\n' "$VERSION" "$repo_slug"
      else
        echo "No publication commands were attempted in this run."
      fi
      if [[ "$artifacts_ready" -eq 1 ]]; then
        echo "Original archives, SHA256SUMS, NOTES.md and formula retained in: $PWD/$dist_dir"
      fi
      if [[ "$tap_prepared" -eq 1 ]]; then
        echo "Homebrew work directory retained for inspection: $tap_dir"
        echo "Verify published checksums and the current tap version before using that formula."
      fi
      echo "Manual recovery: docs/release-recovery.md"
    } >&2
  fi
  [[ -z "$tmp_dir" ]] || rm -rf "$tmp_dir"
  if [[ -n "$tap_dir" && ( "$status" -eq 0 || "$tap_prepared" -eq 0 ) ]]; then
    rm -rf "$tap_dir"
  fi
  exit "$status"
}
trap finish_release EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

./scripts/release-check.sh "$VERSION"
release_phase="validate configuration"
repo_slug="${GITHUB_REPO:-}"
if [[ -z "$repo_slug" ]]; then
  origin_url="$(git remote get-url origin)"
  repo_slug="$(python3 - "$origin_url" <<'PY'
import re, sys
match = re.fullmatch(r'(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([^/]+/[^/]+?)(?:\.git)?/?', sys.argv[1])
if not match:
    raise SystemExit('error: origin is not a supported GitHub URL; set GITHUB_REPO explicitly')
print(match[1])
PY
)"
fi
tap_repo="${HOMEBREW_TAP_REPO:-agisilaos/homebrew-tap}"
tap_url="${HOMEBREW_TAP_URL:-https://github.com/${tap_repo}.git}"
tap_branch="${HOMEBREW_TAP_BRANCH:-main}"
formula_path="${HOMEBREW_FORMULA_PATH:-$DEFAULT_FORMULA_PATH}"
formula_desc="${HOMEBREW_DESC:-$DEFAULT_HOMEBREW_DESC}"
formula_license="${HOMEBREW_LICENSE:-$DEFAULT_HOMEBREW_LICENSE}"
formula_test_arg="${HOMEBREW_TEST_ARG:-$DEFAULT_HOMEBREW_TEST_ARG}"
build_pkg="${RELEASE_BUILD_PKG:-$DEFAULT_BUILD_PKG}"
git check-ref-format --branch "$tap_branch" >/dev/null 2>&1 || release_error "invalid tap branch: $tap_branch"
python3 - "$repo_slug" "$tap_repo" "$tap_url" "$formula_path" "$build_pkg" "$formula_desc" "$formula_license" "$formula_test_arg" <<'PY'
import re, shlex, sys
from pathlib import PurePosixPath
repo, tap, url, formula, package, desc, license, test = sys.argv[1:]
for slug in (repo, tap):
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*', slug):
        raise SystemExit(f'error: invalid repository owner/name: {slug}')
if not url or url.startswith('-') or any(c.isspace() or ord(c) < 32 for c in url):
    raise SystemExit('error: invalid Homebrew tap URL')
parts = formula.split('/')
if formula.startswith('/') or any(p in ('', '.', '..') for p in parts) or not formula.endswith('.rb'):
    raise SystemExit('error: formula path must be a safe relative .rb path')
if not re.fullmatch(r'[A-Za-z0-9_./-]+', formula):
    raise SystemExit('error: invalid characters in formula path')
if package != '.' and (not package.startswith('./') or any(p in ('', '..') for p in package[2:].split('/')) or not re.fullmatch(r'\./[A-Za-z0-9_./-]+', package)):
    raise SystemExit('error: build package must be a local relative package without traversal')
for value in (desc, license, test):
    if any(ord(c) < 32 for c in value):
        raise SystemExit('error: formula metadata contains a control character')
try:
    if not shlex.split(test):
        raise ValueError('empty version arguments')
except ValueError as exc:
    raise SystemExit(f'error: invalid formula version arguments: {exc}')
PY
[[ -d "$build_pkg" ]] || release_error "build package directory not found: $build_pkg"

version_no_v="${VERSION#v}"
commit_short="$(git rev-parse --short=12 HEAD)"
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ldflags="${RELEASE_LDFLAGS:-$RELEASE_LDFLAGS_TEMPLATE}"
ldflags="${ldflags//\{\{VERSION\}\}/$VERSION}"
ldflags="${ldflags//\{\{COMMIT\}\}/$commit_short}"
ldflags="${ldflags//\{\{DATE\}\}/$build_date}"
python3 - "$RELEASE_LDFLAGS_TEMPLATE" "$ldflags" "$VERSION" "$commit_short" "$build_date" <<'PY'
import shlex, sys
template, actual, version, commit, date = sys.argv[1:]
def stamps(value):
    tokens = shlex.split(value)
    result = {}
    for i, token in enumerate(tokens):
        if token == '-X' and i + 1 < len(tokens):
            symbol, separator, stamp = tokens[i + 1].partition('=')
            if separator:
                result[symbol] = stamp
        elif token.startswith('-X='):
            symbol, separator, stamp = token[3:].partition('=')
            if separator:
                result[symbol] = stamp
    return result
configured = stamps(template)
provided = stamps(actual)
for marker, value in [('{{VERSION}}', version), ('{{COMMIT}}', commit), ('{{DATE}}', date)]:
    required = {symbol: stamp.replace(marker, value) for symbol, stamp in configured.items() if marker in stamp}
    if not required or any(provided.get(symbol) != stamp for symbol, stamp in required.items()):
        raise SystemExit(f'error: release ldflags must include configured {marker} metadata')
PY

release_phase="prepare artifacts"
python3 - "$dist_dir" "$ARTIFACT_NAME" "$version_no_v" "$formula_path" <<'PY'
import sys
from pathlib import Path
root, artifact, version, formula = sys.argv[1:]
targets = ['NOTES.md', 'SHA256SUMS', f'{artifact}_{version}_darwin_amd64.tar.gz',
           f'{artifact}_{version}_darwin_arm64.tar.gz', 'homebrew/' + formula]
for target in targets:
    cursor = Path(root)
    pieces = target.split('/')
    for index, part in enumerate([''] + pieces):
        if part:
            cursor /= part
        if cursor.is_symlink():
            raise SystemExit('error: release output path contains a symlink')
        if cursor.exists() and ((index < len(pieces) and not cursor.is_dir()) or
                                (index == len(pieces) and not cursor.is_file())):
            raise SystemExit('error: release output path has an unexpected type')
PY
mkdir -p "$dist_dir"
tmp_dir="$(mktemp -d)"
host_settings="$(go env GOHOSTOS GOHOSTARCH)"
host_os="${host_settings%%$'\n'*}"
host_arch="${host_settings#*$'\n'}"
if [[ "$RELEASE_INCLUDE_LICENSE" -eq 1 ]]; then
  [[ -s LICENSE ]] || release_error "LICENSE is required for release archives"
  cp LICENSE "$tmp_dir/LICENSE"
fi

build_archive() {
  local arch="$1"
  local bin_path="$tmp_dir/$CLI_NAME"
  local archive_path="$dist_dir/${ARTIFACT_NAME}_${version_no_v}_darwin_${arch}.tar.gz"
  rm -f "$bin_path"
  GOOS=darwin GOARCH="$arch" CGO_ENABLED="$RELEASE_CGO_ENABLED" \
    go build -trimpath -ldflags "$ldflags" -o "$bin_path" "$build_pkg"
  [[ -s "$bin_path" && -x "$bin_path" ]] || release_error "build did not produce an executable binary: $arch"
  go version -m "$bin_path" > "$tmp_dir/build-metadata.txt"
  python3 - "$tmp_dir/build-metadata.txt" "$arch" "$RELEASE_CGO_ENABLED" "$ldflags" \
    "$bin_path" "$VERSION" "$commit_short" "$build_date" <<'PY'
import shlex, sys
from pathlib import Path
metadata, arch, cgo, flags, binary, version, commit, date = sys.argv[1:]
settings = {}
for line in Path(metadata).read_text().splitlines():
    if line.strip().startswith('build\t'):
        field = line.strip().split('\t', 1)[1]
        key, separator, value = field.partition('=')
        if separator:
            settings[key] = shlex.split(value)[0] if value.startswith('"') else value
for key, expected in [('GOOS', 'darwin'), ('GOARCH', arch), ('CGO_ENABLED', cgo)]:
    if settings.get(key) != expected:
        raise SystemExit(f'error: built artifact metadata mismatch for {key}: expected {expected!r}, got {settings.get(key)!r}')
# Recent Go toolchains omit linker flags from build info with -trimpath.
if '-ldflags' in settings and settings['-ldflags'] != flags:
    raise SystemExit('error: built artifact metadata mismatch for -ldflags')
content = Path(binary).read_bytes()
for label, value in [('version', version), ('commit', commit), ('date', date)]:
    if value.encode() not in content:
        raise SystemExit(f'error: built artifact is missing its {label} stamp')
PY
  if [[ "$host_os" == darwin && "$host_arch" == "$arch" ]]; then
    version_template="$CLI_NAME {{VERSION}} ({{COMMIT}}) {{DATE}}"
    version_template="${RELEASE_VERSION_TEMPLATE:-$version_template}"
    expected_version="${version_template//\{\{VERSION\}\}/$VERSION}"
    expected_version="${expected_version//\{\{COMMIT\}\}/$commit_short}"
    expected_version="${expected_version//\{\{DATE\}\}/$build_date}"
    python3 - "$bin_path" "$DEFAULT_HOMEBREW_TEST_ARG" "$expected_version" "$tmp_dir" <<'PY'
import shlex, subprocess, sys
binary, arguments, expected, scratch = sys.argv[1:]
result = subprocess.run([binary, *shlex.split(arguments)], cwd=scratch,
                        capture_output=True, text=True, timeout=30)
if result.returncode or result.stdout.rstrip('\n') != expected:
    raise SystemExit(f'error: built artifact version output mismatch: {result.stdout!r}; {result.stderr!r}')
PY
  fi
  if [[ "$RELEASE_INCLUDE_LICENSE" -eq 1 ]]; then
    COPYFILE_DISABLE=1 tar -C "$tmp_dir" -czf "$archive_path" "$CLI_NAME" LICENSE
  else
    COPYFILE_DISABLE=1 tar -C "$tmp_dir" -czf "$archive_path" "$CLI_NAME"
  fi
}
build_archive amd64
build_archive arm64
amd64_archive="$dist_dir/${ARTIFACT_NAME}_${version_no_v}_darwin_amd64.tar.gz"
arm64_archive="$dist_dir/${ARTIFACT_NAME}_${version_no_v}_darwin_arm64.tar.gz"
sha_sums_path="$dist_dir/SHA256SUMS"
python3 - "$CLI_NAME" "$RELEASE_INCLUDE_LICENSE" "$sha_sums_path" "$amd64_archive" "$arm64_archive" <<'PY'
import hashlib, sys, tarfile
from pathlib import Path
cli, include_license, sums, *archives = sys.argv[1:]
expected = {cli} | ({'LICENSE'} if include_license == '1' else set())
lines = []
for name in archives:
    archive = Path(name)
    with tarfile.open(archive, 'r:gz') as packed:
        members = packed.getmembers()
        if len(members) != len(expected) or {m.name for m in members} != expected:
            raise SystemExit(f'error: unexpected archive members: {archive.name}')
        if any(not m.isfile() or m.size == 0 for m in members):
            raise SystemExit(f'error: archive contains empty or non-regular members: {archive.name}')
        binary = next(m for m in members if m.name == cli)
        if not binary.mode & 0o111:
            raise SystemExit(f'error: archived binary is not executable: {archive.name}')
    lines.append(f'{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n')
Path(sums).write_text(''.join(lines))
PY

release_phase="prepare release notes"
notes_path="$dist_dir/NOTES.md"
python3 ./scripts/changelog-section.py --version "$VERSION" --extract --require-traceability > "$tmp_dir/notes.md"
[[ -s "$tmp_dir/notes.md" ]] || release_error "release notes are empty"
cp "$tmp_dir/notes.md" "$notes_path"
release_phase="prepare Homebrew formula"
dry_formula_path="$dist_dir/homebrew/$formula_path"
python3 - "$dry_formula_path" "$FORMULA_NAME" "$CLI_NAME" "$formula_desc" "$formula_license" \
  "$formula_test_arg" "$repo_slug" "$VERSION" "$ARTIFACT_NAME" "$sha_sums_path" <<'PY'
import re, shlex, sys
from pathlib import Path
target, formula, cli, desc, license, test, repo, version, artifact, sums = sys.argv[1:]
def ruby(value):
    # Ruby single-quoted strings never evaluate interpolation.
    return "'" + value.replace('\\', '\\\\').replace("'", "\\'") + "'"
checksums = {}
for line in Path(sums).read_text().splitlines():
    checksum, name = line.split('  ', 1)
    checksums[name] = checksum
number = version[1:]
urls = {arch: f'https://github.com/{repo}/releases/download/{version}/{artifact}_{number}_darwin_{arch}.tar.gz' for arch in ('arm64', 'amd64')}
klass = ''.join(part[0].upper() + part[1:] for part in re.split('[-_]', formula))
argv = ' '.join(shlex.quote(part) for part in shlex.split(test))
text = f'''class {klass} < Formula
  desc {ruby(desc)}
  homepage {ruby('https://github.com/' + repo)}
  license {ruby(license)}
  version {ruby(number)}

  on_macos do
    if Hardware::CPU.arm?
      url {ruby(urls['arm64'])}
      sha256 {ruby(checksums[f'{artifact}_{number}_darwin_arm64.tar.gz'])}
    else
      url {ruby(urls['amd64'])}
      sha256 {ruby(checksums[f'{artifact}_{number}_darwin_amd64.tar.gz'])}
    end
  end

  def install
    bin.install {ruby(cli)}
  end

  test do
    assert_match version.to_s, shell_output("\\"#{{bin}}/{cli}\\" " + {ruby(argv)})
  end
end
'''
path = Path(target)
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(text)
PY
ruby -c "$dry_formula_path" >/dev/null || release_error "formula is not valid Ruby"
# Check the actual persisted artifacts and checksum manifest, not only render inputs.
python3 - "$sha_sums_path" "$dry_formula_path" "$amd64_archive" "$arm64_archive" <<'PY'
import hashlib, re, sys
from pathlib import Path
sums, formula, *archives = map(Path, sys.argv[1:])
lines = sums.read_text().splitlines()
if len(lines) != len(archives):
    raise SystemExit('error: checksum manifest has unexpected entries')
expected = []
content = formula.read_text()
for archive in archives:
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    expected.append(f'{digest}  {archive.name}')
    if f"sha256 '{digest}'" not in content or archive.name not in content:
        raise SystemExit('error: formula does not match artifact checksums/URLs')
if lines != expected:
    raise SystemExit('error: artifact checksums do not match SHA256SUMS')
PY
artifacts_ready=1

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "dry-run: validated macOS archives, SHA256SUMS and $notes_path"
  echo "dry-run: rendered and validated Homebrew formula $dry_formula_path"
  echo "dry-run: would create and push tag $VERSION at $release_commit"
  echo "dry-run: would publish GitHub release/assets to $repo_slug"
  echo "dry-run: would clone and update tap branch $tap_branch at $formula_path"
  exit 0
fi

release_phase="clone Homebrew tap"
tap_dir="$(mktemp -d)"
git clone --branch "$tap_branch" --single-branch -- "$tap_url" "$tap_dir"
[[ "$(git -C "$tap_dir" symbolic-ref --quiet --short HEAD)" == "$tap_branch" ]] || release_error "tap clone did not select branch $tap_branch"
release_phase="prepare Homebrew formula"
python3 - "$tap_dir" "$formula_path" <<'PY'
import sys
from pathlib import Path
root = Path(sys.argv[1])
cursor = root
for part in sys.argv[2].split('/'):
    cursor /= part
    if cursor.is_symlink():
        raise SystemExit('error: tap formula path contains a symlink')
if cursor.exists() and not cursor.is_file():
    raise SystemExit('error: tap formula target is not a regular file')
cursor.parent.mkdir(parents=True, exist_ok=True)
PY
cp "$dry_formula_path" "$tap_dir/$formula_path"
tap_prepared=1
release_phase="commit Homebrew formula"
git -C "$tap_dir" add -- "$formula_path"
if ! git -C "$tap_dir" diff --cached --quiet; then
  git -C "$tap_dir" commit -m "${FORMULA_NAME}: ${VERSION}"
fi

# Recheck source identity immediately before the first publication write.
[[ "$(git rev-parse HEAD)" == "$release_commit" ]] || release_error "release commit changed during preparation"
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || release_error "working tree changed during preparation"
if git rev-parse --quiet --verify "refs/tags/$VERSION" >/dev/null 2>&1; then
  release_error "tag $VERSION appeared during preparation"
fi
publication_started=1
release_phase="create local tag"
local_tag_status="outcome unknown (command did not report success)"
git tag "$VERSION" "$release_commit"
local_tag_status="completed (command reported success)"
release_phase="push version tag"
tag_push_status="outcome unknown (command did not report success)"
git push origin "refs/tags/$VERSION:refs/tags/$VERSION"
tag_push_status="completed (command reported success)"
release_phase="publish GitHub release/assets"
github_status="outcome unknown (command did not report success)"
gh release create "$VERSION" "$amd64_archive" "$arm64_archive" "$sha_sums_path" \
  --repo "$repo_slug" --title "$VERSION" --notes-file "$notes_path"
github_status="completed (command reported success)"
release_phase="push Homebrew formula"
homebrew_status="outcome unknown (command did not report success)"
git -C "$tap_dir" push origin "HEAD:refs/heads/$tap_branch"
homebrew_status="completed (command reported success)"
echo "release completed for $VERSION"
