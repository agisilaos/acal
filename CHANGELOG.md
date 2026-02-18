# Changelog

All notable changes to this project will be documented in this file.

## [v0.1.3] - 2026-02-17

### Added

- Recurrence rule validation now enforces a strict grammar for `events add|update --repeat` with canonical normalization (`daily|weekly|monthly|yearly`, bounded counts, weekday validation).
- Reminder verification now performs backend readback after `events remind` writes and fails on mismatch/unverifiable state.
- Batch execution now emits stable transaction metadata:
  - `tx_id` per batch run
  - `op_id` per operation row
- History snapshots now persist transactional identifiers (`tx_id`, `op_id`) for deterministic recovery of partial batch runs.

### Changed

- `events batch` machine output contract now includes `tx_id` in row data and meta.
- JSON golden normalization now scrubs dynamic transaction values to keep contract tests stable.
- Documentation now explicitly covers:
  - strict repeat grammar
  - reminder readback verification behavior
  - transactional batch/history identifiers and schema

## [v0.1.2] - 2026-02-17

- Harden agent workflows, add strict modes, redo, and backend reminder/recurrence fields (523e1ed)
- Add 10-step CLI expansion: planning, ICS, batch, history, and queries (32889e2)
- feat(status): add runtime health/status command with diagnostics (1cd2182)
- feat(recurrence): implement future-scope update/delete in osascript backend (970e289)
- feat(events): add copy command with dry-run and validation (10450ee)
- feat(events): add move command with scope, dry-run, and validation (4eea6cf)
- test(root): cover backend selection and context error branches (2f340e2)
- refactor(events): centralize command error/hint wrapping (63656c9)
- test(events): add validation matrix for update/delete guardrails (1ea09ec)
- test(cli): expand admin/root/printer coverage for safety paths (e265e0d)
- feat(cli): wire verbose diagnostics and no-color behavior (10fa8c9)
- feat(cli): add interactive delete confirmation with non-interactive guardrails (a1f5689)
- refactor(output): inject command writers into printer (f6f5ddd)
- refactor(backend): complete osascript file split and restore green build (b71284c)
- feat(recurrence): add explicit scope handling for update/delete with tests (4bdec11)
- refactor(app): split root command wiring into focused command files (9494f2e)
- docs: add Homebrew install instructions (f9a94e0)
- chore: remove unreleased changelog requirement (99fdd1b)
- chore: relax changelog unreleased requirement in release checks (e0cbcc3)

## [v0.1.1] - 2026-02-16

- Added JSON golden contract tests for key agent-facing commands (`setup`, `today`, `week --summary`, `month`, `quick-add --dry-run`).
- Added CI workflow to run release-check on pull requests and pushes to `main`.
- Updated release automation to create missing GitHub repositories as private by default.
- Documentation updates for implemented commands and release process.

## [v0.1.0] - 2026-02-16

- Initial public CLI baseline with setup, view (`today|week|month`), events CRUD, query, and quick-add.
