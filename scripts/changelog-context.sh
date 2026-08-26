#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ $# -ne 1 ]]; then
  die "usage: scripts/changelog-context.sh vX.Y.Z"
fi

version="$1"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  die "version must match vX.Y.Z (got: $version)"
fi

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "not inside a git work tree"

repo_slug="${GITHUB_REPO:-}"
if [[ -z "$repo_slug" ]]; then
  remote="$(git remote get-url origin 2>/dev/null || true)"
  remote="${remote#git@github.com:}"
  remote="${remote#https://github.com/}"
  remote="${remote%.git}"
  if [[ "$remote" == */* && "$remote" != /* ]]; then
    repo_slug="$remote"
  fi
fi

previous_tag="$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)"
if [[ -n "$previous_tag" ]]; then
  commit_range="${previous_tag}..HEAD"
else
  commit_range="HEAD"
fi

commit_count="$(git rev-list --count "$commit_range")"

echo "# Changelog evidence for $version"
echo
echo "- Previous tag: ${previous_tag:-none}"
echo "- Commit range: \`$commit_range\`"
echo "- Commits: $commit_count"
if [[ -n "$repo_slug" ]]; then
  echo "- Repository: https://github.com/$repo_slug"
  if [[ -n "$previous_tag" ]]; then
    echo "- Compare: https://github.com/$repo_slug/compare/$previous_tag...HEAD"
  fi
fi
echo
echo "Use this evidence to group implementation commits into user-facing outcomes."
echo "Link each changelog bullet to the relevant pull request, or to a commit when no pull request exists."

if [[ "$commit_count" -eq 0 ]]; then
  echo
  echo "No commits found in the release range."
  exit 0
fi

while IFS= read -r sha; do
  short_sha="$(git show -s --format='%h' "$sha")"
  subject="$(git show -s --format='%s' "$sha")"
  author="$(git show -s --format='%an' "$sha")"
  authored="$(git show -s --date=short --format='%ad' "$sha")"
  body="$(git show -s --format='%b' "$sha")"
  pr_numbers="$(printf '%s\n%s\n' "$subject" "$body" | grep -oE '#[0-9]+' | sort -u | tr '\n' ' ' || true)"

  echo
  if [[ -n "$repo_slug" ]]; then
    echo "## [$short_sha](https://github.com/$repo_slug/commit/$sha) — $subject"
  else
    echo "## $short_sha — $subject"
  fi
  echo
  echo "- Author: $author"
  echo "- Date: $authored"
  if [[ -n "$pr_numbers" ]]; then
    echo "- PR candidates: $pr_numbers"
  fi
  echo "- Changed files:"
  while IFS= read -r file; do
    [[ -n "$file" ]] && echo "  - \`$file\`"
  done < <(git diff-tree --no-commit-id --name-only -r "$sha")
  if [[ -n "$body" ]]; then
    echo
    echo "Commit body:"
    echo
    printf '%s\n' "$body"
  fi
done < <(git rev-list --reverse "$commit_range")
