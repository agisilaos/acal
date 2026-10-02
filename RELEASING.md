# Releasing

## Go toolchain

Release checks, dry runs and publication select Go 1.27.1 through
`RELEASE_GO_TOOLCHAIN` in `scripts/release-config.sh`. Release/current CI uses
that same version, and `go.mod` requires Go 1.27.1. Go downloads and verifies
a required toolchain when automatic toolchain selection is enabled.

Releases are prepared by an agent, reviewed by a human, and published from a clean macOS checkout of the default branch.

## Local verification

Run `make verify` during development. It checks the pinned local helper bundle,
module metadata, portability, formatting, static analysis, tests, docs, all
command help snapshots and the native proof fixtures without Calendar access.
Module validation uses disposable metadata and leaves `go.mod` and `go.sum`
unchanged. Native package, Homebrew upgrade and live Calendar smoke qualification
remain separate opt-in workflows described in the [native proof guide](docs/native-proof.md).

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

`release-check` validates the clean worktree, version and changelog, runs `make verify`, then checks the version-stamped binary. `release-dry-run` builds both macOS archives with their license and checksums, extracts the approved changelog section as release notes, and renders and syntax-checks the Homebrew formula without remote writes. Publication requires the `main` branch and validates the selected existing tap branch before creating a tag.

Before the final publish command, exercise the candidate binary extracted from
the dry-run archive on a Mac with Calendar Automation permission and Full Calendar
Access for the invoking app:

```bash
python3 scripts/calendar-smoke.py --binary /absolute/path/to/extracted/acal --allow-calendar-writes
```

This opt-in check creates one uniquely named calendar, uses isolated CLI state,
verifies add/show/update/reminder/undo/redo/delete, and removes only its own
calendar in cleanup. It prints an evidence directory containing the binary
identity, command results, and cleanup result. A failure exits nonzero; inspect
that directory before retrying. The default per-call timeout is 90 seconds for
machines using the slower AppleScript read fallback. Routine CI never runs this
live check. For native Calendar changes, record the tested macOS version and
include a supported stable macOS host in release qualification; a beta-host pass
alone does not establish stable-system behavior.

The final command creates and pushes the tag, publishes the GitHub Release with the approved changelog section, and updates the configured Homebrew tap.

Before publishing, verify that Git `origin`, the repository selected by `gh`,
and any `GITHUB_REPO` override identify the intended release repository. Tag
pushes use `origin`, `gh release create` uses the explicit intended repository, and generated
download URLs use `GITHUB_REPO` or the repository derived from `origin`. The
Homebrew tap has its own `HOMEBREW_TAP_URL` / `HOMEBREW_TAP_REPO` and branch settings.
Human review is a workflow requirement; the script does not record approval or
bind a dry run to a later publish. Keep the reviewed source, notes, and target
configuration unchanged between those steps.

If publication stops, follow [release recovery](docs/release-recovery.md) using
the original retained artifacts and reported phase outcomes. Inspect remote state
before attempting a missing step.

## Changelog policy

- Keep concrete release headings in the form `## [vX.Y.Z] - YYYY-MM-DD`.
- Do not add an `Unreleased` section.
- Treat the reviewed changelog section as the source of truth for GitHub release notes.
