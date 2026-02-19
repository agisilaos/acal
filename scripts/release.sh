#!/usr/bin/env zsh

set -e
set -u
setopt pipefail

cd "$(dirname "$0")/.."

die() { print -u2 -- "error: $*"; exit 1; }

if [[ "$(uname -s)" != "Darwin" ]]; then
  die "release.sh must be run on macOS (Darwin)"
fi

command -v go >/dev/null 2>&1 || die "go is required"
command -v python3 >/dev/null 2>&1 || die "python3 is required"
command -v git >/dev/null 2>&1 || die "git is required"
command -v gh >/dev/null 2>&1 || die "gh is required"

dry_run=0
version=""
for arg in "$@"; do
  case "$arg" in
    --dry-run)
      dry_run=1
      ;;
    *)
      if [[ -z "$version" ]]; then
        version="$arg"
      else
        die "usage: scripts/release.sh vX.Y.Z [--dry-run]"
      fi
      ;;
  esac
done

if [[ -z "${version}" ]]; then
  die "usage: scripts/release.sh vX.Y.Z [--dry-run]"
fi
if [[ "${version}" != v* ]]; then
  version="v${version}"
fi
if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  die "version must match vX.Y.Z (got: ${version})"
fi

repo_owner="agisilaos"
repo_name="acal"
origin_url_default="git@github.com:${repo_owner}/${repo_name}.git"
origin_url="${ORIGIN_URL:-$origin_url_default}"

# Homebrew tap repo name convention: homebrew-<tap> => brew tap <owner>/<tap>
tap_repo="${HOMEBREW_TAP_REPO:-${repo_owner}/homebrew-tap}"
tap_remote_default="git@github.com:${tap_repo}.git"
tap_remote="${HOMEBREW_TAP_ORIGIN_URL:-$tap_remote_default}"

ensure_git_repo() {
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    return 0
  fi
  print -- "Initializing git repo..."
  git init -b main
  git add .
  git commit -m "chore: initial import"
}

ensure_origin_remote() {
  if git remote get-url origin >/dev/null 2>&1; then
    return 0
  fi
  print -- "Adding origin remote: ${origin_url}"
  git remote add origin "${origin_url}"
}

ensure_github_repo() {
  if ! command -v gh >/dev/null 2>&1; then
    print -- "gh not found; skipping GitHub repo creation"
    return 0
  fi
  if gh repo view "${repo_owner}/${repo_name}" >/dev/null 2>&1; then
    return 0
  fi
  print -- "Creating GitHub repo ${repo_owner}/${repo_name} (private)..."
  gh repo create "${repo_owner}/${repo_name}" --private --confirm >/dev/null
}

require_clean_tree() {
  git diff --quiet || die "working tree has unstaged changes"
  git diff --cached --quiet || die "index has staged changes"
}

last_tag() {
  git describe --tags --abbrev=0 2>/dev/null || true
}

generate_release_notes() {
  local prev_tag="$1"
  if [[ -n "${prev_tag}" ]]; then
    git log --no-merges --pretty=format:'- %s (%h)' "${prev_tag}..HEAD"
  else
    git log --no-merges --pretty=format:'- %s (%h)'
  fi
}

update_changelog() {
  local ver="$1"
  local prev_tag="$2"
  local date_utc
  date_utc="$(date -u +%Y-%m-%d)"

  [[ -f CHANGELOG.md ]] || die "CHANGELOG.md not found"

  local notes
  notes="$(generate_release_notes "${prev_tag}")"
  if [[ -z "${notes}" ]]; then
    notes="- No changes recorded."
  fi

  python3 - "$ver" "$date_utc" "$notes" <<'PY'
import sys

version=sys.argv[1]
date=sys.argv[2]
notes=sys.argv[3]

path="CHANGELOG.md"
lines=open(path,"r",encoding="utf-8").read().splitlines()

target_header=f"## [{version}]"
if any(line.startswith(target_header) for line in lines):
    raise SystemExit(f"error: {version} already exists in CHANGELOG.md")

out=[]
insert_at = None
for i, line in enumerate(lines):
    if line.startswith("## ["):
        insert_at = i
        break

if insert_at is None:
    out = lines[:] + ["", f"## [{version}] - {date}", ""] + (notes.splitlines() if notes.strip() else ["- No changes recorded."]) + [""]
else:
    out = lines[:insert_at] + [f"## [{version}] - {date}", ""] + (notes.splitlines() if notes.strip() else ["- No changes recorded."]) + [""] + lines[insert_at:]

open(path,"w",encoding="utf-8").write("\n".join(out).rstrip() + "\n")
PY

  git add CHANGELOG.md
}

build_dist() {
  local ver="$1"
  local commit
  commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  local date_utc
  date_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  rm -rf dist
  mkdir -p dist

  build_one() {
    local goarch="$1"
    local stage="dist/stage_${goarch}"
    rm -rf "${stage}"
    mkdir -p "${stage}"

    GOOS=darwin GOARCH="${goarch}" CGO_ENABLED=0 \
      go build -trimpath \
        -ldflags "-s -w -X main.version=${ver} -X main.commit=${commit} -X main.date=${date_utc}" \
        -o "${stage}/acal" \
        ./cmd/acal

    (cd "${stage}" && tar -czf "../acal_${ver}_darwin_${goarch}.tar.gz" acal)
    rm -rf "${stage}"
  }

  build_one arm64
  build_one amd64

  (cd dist && shasum -a 256 *.tar.gz > SHA256SUMS.txt)
}

create_github_release() {
  local ver="$1"
  local notes_file
  notes_file="$(mktemp)"
  python3 - "$ver" >"${notes_file}" <<'PY'
import sys
ver=sys.argv[1]
txt=open("CHANGELOG.md","r",encoding="utf-8").read().splitlines()
out=[]
in_sec=False
for line in txt:
    if line.startswith("## [") and line.startswith(f"## [{ver}]"):
        in_sec=True
        continue
    if in_sec and line.startswith("## ["):
        break
    if in_sec:
        out.append(line)
print("\n".join(out).strip() or f"Release {ver}")
PY

  gh release create "${ver}" dist/*.tar.gz dist/SHA256SUMS.txt --notes-file "${notes_file}" --latest --target "$(git rev-parse HEAD)"
  rm -f "${notes_file}"
}

ensure_homebrew_tap_repo() {
  if gh repo view "${tap_repo}" >/dev/null 2>&1; then
    return 0
  fi
  print -- "Creating Homebrew tap repo ${tap_repo} (public)..."
  gh repo create "${tap_repo}" --public --confirm >/dev/null
  return 0
}

update_homebrew_formula() {
  local ver="$1"
  local ver_nov="${ver#v}"
  ensure_homebrew_tap_repo

  local sha_arm sha_amd
  sha_arm="$(awk -v f="acal_${ver}_darwin_arm64.tar.gz" '$2==f{print $1}' dist/SHA256SUMS.txt | head -n 1 || true)"
  sha_amd="$(awk -v f="acal_${ver}_darwin_amd64.tar.gz" '$2==f{print $1}' dist/SHA256SUMS.txt | head -n 1 || true)"
  [[ -n "${sha_arm}" && -n "${sha_amd}" ]] || die "failed to parse SHA256SUMS.txt for ${ver}"

  local tmp
  tmp="$(mktemp -d)"

  git clone "${tap_remote}" "${tmp}/tap" >/dev/null 2>&1 || die "failed to clone tap repo: ${tap_remote}"
  mkdir -p "${tmp}/tap/Formula"

  cat >"${tmp}/tap/Formula/acal.rb" <<RUBY
class Acal < Formula
  desc "Apple Calendar CLI for terminal workflows and agents"
  homepage "https://github.com/${repo_owner}/${repo_name}"
  version "${ver_nov}"

  on_macos do
    on_arm do
      url "https://github.com/${repo_owner}/${repo_name}/releases/download/${ver}/acal_${ver}_darwin_arm64.tar.gz"
      sha256 "${sha_arm}"
    end
    on_intel do
      url "https://github.com/${repo_owner}/${repo_name}/releases/download/${ver}/acal_${ver}_darwin_amd64.tar.gz"
      sha256 "${sha_amd}"
    end
  end

  def install
    bin.install "acal"
  end

  test do
    system "#{bin}/acal", "version"
  end
end
RUBY

  python3 - <<'PY' "${tmp}/tap"
import pathlib, sys

tap_dir = pathlib.Path(sys.argv[1])
readme = tap_dir / "README.md"
formula_dir = tap_dir / "Formula"

formulae = sorted(p.stem for p in formula_dir.glob("*.rb") if p.is_file())
if "acal" not in formulae:
    formulae.append("acal")
    formulae.sort()

def read_text(p: pathlib.Path) -> str:
    return p.read_text(encoding="utf-8") if p.exists() else ""

def write_text(p: pathlib.Path, s: str) -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(s.rstrip() + "\n", encoding="utf-8")

text = read_text(readme)
lines = text.splitlines() if text.strip() else ["# agisilaos/homebrew-tap", "", "Homebrew formulae for agisilaos.", ""]

out = []
skipping = False
for line in lines:
    if line.strip().lower() == "## install":
        skipping = True
        continue
    if skipping and line.startswith("## "):
        skipping = False
        out.append(line)
        continue
    if skipping:
        continue
    out.append(line)

install_section = [
    "## Install",
    "",
    "```bash",
    "brew tap agisilaos/tap",
]
install_section.extend([f"brew install {f}" for f in formulae])
install_section.append("```")
install_section.append("")

insert_at = None
for i, line in enumerate(out):
    if line.startswith("## "):
        insert_at = i
        break
if insert_at is None:
    out.extend([""] + install_section)
else:
    out = out[:insert_at] + install_section + out[insert_at:]

write_text(readme, "\n".join(out))
PY

  pushd "${tmp}/tap" >/dev/null
  if [[ "$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo)" != "main" ]]; then
    git checkout -B main >/dev/null 2>&1 || git checkout -b main
  fi
  git add Formula/acal.rb README.md
  if git diff --cached --quiet; then
    print -- "Homebrew formula already up to date"
    popd >/dev/null
    return 0
  fi
  git commit -m "acal ${ver}"
  git push origin HEAD:main
  popd >/dev/null

  print -- "Updated Homebrew formula in ${tap_repo}"
  rm -rf "${tmp}"
}

main() {
  ensure_git_repo
  ensure_github_repo
  ensure_origin_remote
  require_clean_tree

  if git rev-parse "${version}" >/dev/null 2>&1; then
    die "tag already exists: ${version}"
  fi

  local prev
  prev="$(last_tag)"

  print -- "Running release checks..."
  ./scripts/release-check.sh "${version}"

  if [[ "${dry_run}" -eq 1 ]]; then
    print -- "Dry run enabled: changelog/tag/push/release/tap updates will be skipped."
    print -- "Building dist artifacts..."
    build_dist "${version}"
    print -- "Dry run complete. Artifacts: dist/"
    return 0
  fi

  update_changelog "${version}" "${prev}"
  git commit -m "chore(release): ${version}"

  print -- "Building dist artifacts..."
  build_dist "${version}"

  if ! git rev-parse "${version}" >/dev/null 2>&1; then
    git tag "${version}"
  fi

  print -- "Pushing main commit..."
  git push origin main

  print -- "Pushing release tag..."
  git push origin "${version}"

  print -- "Creating GitHub release..."
  create_github_release "${version}"

  print -- "Updating Homebrew tap..."
  update_homebrew_formula "${version}"

  print -- "Done. Artifacts: dist/"
}

main
