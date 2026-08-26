# Releasing

Releases are prepared by an agent, reviewed by a human, and published from a clean macOS checkout of the default branch.

## Prepare the changelog

Ask an agent to prepare `vX.Y.Z`. The agent must start from the repository evidence:

```bash
make changelog-context VERSION=vX.Y.Z
```

The agent updates only the new top section of `CHANGELOG.md` and must:

- describe user-visible outcomes rather than copy commit subjects;
- group related implementation commits into one useful bullet;
- use clear headings such as `Added`, `Changed`, `Fixed`, or `Removed` when they help;
- link every bullet to its verified merged GitHub pull request, or to a GitHub commit when no pull request exists;
- call out breaking changes explicitly;
- preserve all existing release sections.

Use commit messages, changed-file evidence, and PR metadata to understand impact. Never infer a PR association without evidence. Review the generated section, then commit it before running release checks.

## Validate and publish

Run these commands in order:

```bash
make release-check VERSION=vX.Y.Z
make release-dry-run VERSION=vX.Y.Z
make release VERSION=vX.Y.Z
```

`release-check` validates the clean worktree, version, changelog, tests, documentation, module metadata, formatting, and version-stamped binary. `release-dry-run` builds both macOS archives and checksums, extracts the approved changelog section as release notes, and renders the Homebrew formula without remote writes.

The final command creates and pushes the tag, publishes the GitHub Release with the approved changelog section, and updates the configured Homebrew tap.

## Changelog policy

- Keep concrete release headings in the form `## [vX.Y.Z] - YYYY-MM-DD`.
- Do not add an `Unreleased` section.
- Treat the reviewed changelog section as the source of truth for GitHub release notes.
