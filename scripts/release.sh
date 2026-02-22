#!/usr/bin/env bash
set -euo pipefail

CLI_NAME="acal"
FORMULA_NAME="acal"
ARTIFACT_NAME="acal"
DEFAULT_BRANCH="main"
DEFAULT_HOMEBREW_DESC="Apple Calendar CLI for terminal workflows and agents"
DEFAULT_HOMEBREW_LICENSE="MIT"
DEFAULT_HOMEBREW_TEST_ARG="version"
DEFAULT_FORMULA_PATH="Formula/acal.rb"
DEFAULT_BUILD_PKG=""
RELEASE_LDFLAGS_TEMPLATE='-s -w -X main.version={{VERSION}} -X main.commit={{COMMIT}} -X main.date={{DATE}}'

usage() {
  cat <<USAGE
Usage:
  ./scripts/release.sh [--dry-run] vX.Y.Z

Environment:
  HOMEBREW_TAP_REPO      Tap repo in owner/name format (default: agisilaos/homebrew-tap)
  HOMEBREW_TAP_BRANCH    Tap branch to push (default: main)
  HOMEBREW_FORMULA_PATH  Path in tap repo (default: ${DEFAULT_FORMULA_PATH})
  GITHUB_REPO            owner/name for release URL generation (auto-detected from git remote)
  HOMEBREW_DESC          Formula description text
  HOMEBREW_LICENSE       Formula license (default: ${DEFAULT_HOMEBREW_LICENSE})
  HOMEBREW_TEST_ARG      Formula test version arg (default: ${DEFAULT_HOMEBREW_TEST_ARG})
  RELEASE_BUILD_PKG      Go build package path override (default: auto cmd/<cli> or .)
  RELEASE_LDFLAGS        Optional ldflags override
USAGE
}

err() {
  echo "error: $*" >&2
  exit 1
}

to_class_name() {
  echo "$1" | awk -F'[-_]' '{
    for (i = 1; i <= NF; i++) {
      $i = toupper(substr($i, 1, 1)) substr($i, 2)
    }
    OFS = ""
    print $0
  }'
}

detect_repo_slug() {
  local remote
  remote="$(git remote get-url origin)"
  remote="${remote#git@github.com:}"
  remote="${remote#https://github.com/}"
  remote="${remote%.git}"
  echo "$remote"
}

checksum_file() {
  local file="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  else
    err "neither shasum nor sha256sum is available"
  fi
}

DRY_RUN=0
VERSION=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    v*)
      if [[ -n "$VERSION" ]]; then
        err "version provided multiple times"
      fi
      VERSION="$1"
      shift
      ;;
    *)
      err "unknown argument: $1"
      ;;
  esac
done

if [[ -z "$VERSION" ]]; then
  err "usage: ./scripts/release.sh [--dry-run] vX.Y.Z"
fi

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  err "invalid VERSION '$VERSION' (expected vX.Y.Z)"
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  err "must run inside a git repository"
fi

current_branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
if [[ -n "$current_branch" && "$current_branch" != "$DEFAULT_BRANCH" ]]; then
  echo "warning: current branch is $current_branch, expected $DEFAULT_BRANCH" >&2
fi

if [[ -n "$(git status --porcelain)" ]]; then
  err "working tree is not clean"
fi

./scripts/release-check.sh "$VERSION"

for cmd in go git gh tar; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    err "required command not found: $cmd"
  fi
done

version_no_v="${VERSION#v}"
dist_dir="dist"
mkdir -p "$dist_dir"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

if ! git rev-parse -q --verify HEAD >/dev/null 2>&1; then
  err "repository has no commits yet; create an initial commit before running release scripts"
fi

build_pkg="${DEFAULT_BUILD_PKG}"
if [[ -z "$build_pkg" ]]; then
  build_pkg="./cmd/${CLI_NAME}"
fi
if [[ ! -d "$build_pkg" ]]; then
  build_pkg="."
fi
build_pkg="${RELEASE_BUILD_PKG:-$build_pkg}"

commit_short="$(git rev-parse --short HEAD)"
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ldflags_template="${RELEASE_LDFLAGS:-${RELEASE_LDFLAGS_TEMPLATE}}"
ldflags="${ldflags_template//\{\{VERSION\}\}/$VERSION}"
ldflags="${ldflags//\{\{COMMIT\}\}/$commit_short}"
ldflags="${ldflags//\{\{DATE\}\}/$build_date}"

build_archive() {
  local arch="$1"
  local bin_path="$tmp_dir/$CLI_NAME"
  local archive_path="$dist_dir/${ARTIFACT_NAME}_${version_no_v}_darwin_${arch}.tar.gz"
  local -a build_cmd

  build_cmd=(go build -trimpath -o "$bin_path" "$build_pkg")
  if [[ -n "$ldflags" ]]; then
    build_cmd=(go build -trimpath -ldflags "$ldflags" -o "$bin_path" "$build_pkg")
  fi

  rm -f "$bin_path"
  GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 "${build_cmd[@]}"
  tar -C "$tmp_dir" -czf "$archive_path" "$CLI_NAME"
}

build_archive amd64
build_archive arm64

amd64_archive="$dist_dir/${ARTIFACT_NAME}_${version_no_v}_darwin_amd64.tar.gz"
arm64_archive="$dist_dir/${ARTIFACT_NAME}_${version_no_v}_darwin_arm64.tar.gz"
sha_sums_path="$dist_dir/SHA256SUMS"

amd64_sha="$(checksum_file "$amd64_archive")"
arm64_sha="$(checksum_file "$arm64_archive")"

cat <<SUMS > "$sha_sums_path"
${amd64_sha}  ${ARTIFACT_NAME}_${version_no_v}_darwin_amd64.tar.gz
${arm64_sha}  ${ARTIFACT_NAME}_${version_no_v}_darwin_arm64.tar.gz
SUMS

notes=""
if prev_tag="$(git describe --tags --abbrev=0 2>/dev/null)"; then
  notes="$(git log --pretty='- %s (%h)' "${prev_tag}..HEAD" 2>/dev/null || true)"
else
  notes="$(git log --pretty='- %s (%h)' 2>/dev/null || true)"
fi

if [[ -z "$notes" ]]; then
  notes="- No user-facing changes"
fi

if git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null 2>&1; then
  err "tag $VERSION already exists"
fi

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "dry-run: would create tag $VERSION"
  echo "dry-run: would create GitHub release for $VERSION"
  echo "dry-run: would upload $amd64_archive"
  echo "dry-run: would upload $arm64_archive"
  echo "dry-run: would upload $sha_sums_path"
  echo "dry-run: would update Homebrew formula ${FORMULA_NAME}.rb"
  exit 0
fi

repo_slug="${GITHUB_REPO:-$(detect_repo_slug)}"
if [[ -z "$repo_slug" ]]; then
  err "could not determine GitHub repo slug"
fi

git tag "$VERSION"
git push origin "$VERSION"

gh release create "$VERSION" \
  "$amd64_archive" \
  "$arm64_archive" \
  "$sha_sums_path" \
  --title "$VERSION" \
  --notes "$notes"

tap_repo="${HOMEBREW_TAP_REPO:-agisilaos/homebrew-tap}"
tap_branch="${HOMEBREW_TAP_BRANCH:-main}"
formula_path="${HOMEBREW_FORMULA_PATH:-${DEFAULT_FORMULA_PATH}}"
formula_class="$(to_class_name "$FORMULA_NAME")"
formula_desc="${HOMEBREW_DESC:-${DEFAULT_HOMEBREW_DESC}}"
formula_license="${HOMEBREW_LICENSE:-${DEFAULT_HOMEBREW_LICENSE}}"
formula_test_arg="${HOMEBREW_TEST_ARG:-${DEFAULT_HOMEBREW_TEST_ARG}}"

tap_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir" "$tap_dir"' EXIT

git clone "git@github.com:${tap_repo}.git" "$tap_dir"
mkdir -p "$(dirname "$tap_dir/$formula_path")"

cat <<FORMULA > "$tap_dir/$formula_path"
class ${formula_class} < Formula
  desc "${formula_desc}"
  homepage "https://github.com/${repo_slug}"
  license "${formula_license}"
  version "${version_no_v}"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/${repo_slug}/releases/download/${VERSION}/${ARTIFACT_NAME}_${version_no_v}_darwin_arm64.tar.gz"
      sha256 "${arm64_sha}"
    else
      url "https://github.com/${repo_slug}/releases/download/${VERSION}/${ARTIFACT_NAME}_${version_no_v}_darwin_amd64.tar.gz"
      sha256 "${amd64_sha}"
    end
  end

  def install
    bin.install "${CLI_NAME}"
  end

  test do
    shell_output("#{bin}/${CLI_NAME} ${formula_test_arg}")
  end
end
FORMULA

(
  cd "$tap_dir"
  git add "$formula_path"
  if ! git diff --cached --quiet; then
    git commit -m "${FORMULA_NAME}: ${VERSION}"
    git push origin HEAD:"$tap_branch"
  else
    echo "Homebrew formula already up to date"
  fi
)

echo "release completed for $VERSION"
