#!/usr/bin/env bash
set -euo pipefail

# Repository-owned wrappers select the consumer root. Do not derive it from
# this helper's path: copied helpers live one directory deeper than wrappers.
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ $# -eq 1 ]] || die 'usage: changelog-context.sh vX.Y.Z (run from repository root)'
version="$1"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "version must match vX.Y.Z (got: $version)"
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die 'not inside a Git worktree'

repo_slug="${GITHUB_REPO:-}"
if [[ -z "$repo_slug" ]]; then
  remote="$(git remote get-url origin 2>/dev/null || true)"
  remote="${remote#git@github.com:}"
  remote="${remote#https://github.com/}"
  remote="${remote%.git}"
  if [[ "$remote" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
    repo_slug="$remote"
  fi
fi
if [[ -n "$repo_slug" && ! "$repo_slug" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
  die 'GITHUB_REPO must be a GitHub owner/repository slug'
fi

previous_tag="$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)"
commit_range="HEAD"
[[ -z "$previous_tag" ]] || commit_range="${previous_tag}..HEAD"
commit_count="$(git rev-list --count "$commit_range")"

printf '# Changelog evidence for %s\n\n' "$version"
printf -- '- Previous tag: %s\n- Commit range: `%s`\n- Commits: %s\n' "${previous_tag:-none}" "$commit_range" "$commit_count"
if [[ -n "$repo_slug" ]]; then
  printf -- '- Repository: https://github.com/%s\n' "$repo_slug"
  [[ -z "$previous_tag" ]] || printf -- '- Compare: https://github.com/%s/compare/%s...HEAD\n' "$repo_slug" "$previous_tag"
fi
printf '\nUse this evidence to group implementation commits into user-facing outcomes.\n'
printf 'Link each changelog list item to the relevant pull request, or to a commit when no pull request exists.\n'
if [[ "$commit_count" -eq 0 ]]; then
  printf '\nNo commits found in the release range.\n'
  exit 0
fi

while IFS= read -r sha; do
  short_sha="$(git show -s --format='%h' "$sha")"
  subject="$(git show -s --format='%s' "$sha")"
  author="$(git show -s --format='%an' "$sha")"
  authored="$(git show -s --date=short --format='%ad' "$sha")"
  body="$(git show -s --format='%b' "$sha")"
  pr_numbers="$(printf '%s\n%s\n' "$subject" "$body" | grep -oE '#[0-9]+' | sort -u | tr '\n' ' ' || true)"
  if [[ -n "$repo_slug" ]]; then
    printf '\n## [%s](https://github.com/%s/commit/%s) — %s\n\n' "$short_sha" "$repo_slug" "$sha" "$subject"
  else
    printf '\n## %s — %s\n\n' "$short_sha" "$subject"
  fi
  printf -- '- Author: %s\n- Date: %s\n' "$author" "$authored"
  [[ -z "$pr_numbers" ]] || printf -- '- PR candidates: %s\n' "$pr_numbers"
  printf -- '- Changed files:\n'
  while IFS= read -r file; do
    [[ -z "$file" ]] || printf '  - `%s`\n' "$file"
  done < <(git diff-tree --root --no-commit-id --name-only -r "$sha")
  [[ -z "$body" ]] || printf '\nCommit body:\n\n%s\n' "$body"
done < <(git rev-list --reverse "$commit_range")
