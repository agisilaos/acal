# Changelog

All notable changes to this project will be documented in this file.

## [v0.2.1] - 2026-02-18

- perf: reuse sqlite read handles and improve timeout diagnostics (7cbc317)
- test(perf): add sqlite read benchmark and fixture coverage (11cdad8)
- perf(backend): harden sqlite reads and avoid fallback on context cancel (3e209b4)
- perf(backend): use database/sql for sqlite calendar reads (1142211)
- perf(backend): push calendar and text predicates into sqlite (c808b27)
- perf(backend): push event limit into sqlite query (43bdeee)

## [v0.2.0] - 2026-02-18

- cli: add degraded gating, status explain, timings, and fast history paging (5245e56)
- cli: improve health UX, plain output, and history pagination (f8403a9)
- app: add global timeout and propagate cancellable contexts (dc2d870)
- backend: enforce context cancellation for osascript and sqlite3 (a142e56)
- status: report effective resolved output mode (4c53699)
- test: lock in stderr rendering for silent CLI failures (be503cb)
- cli: make auto output mode TTY-aware (8fe5cc5)
- cli: make doctor emit a single payload on failure (01aaf32)
- cli: validate delete safety before backend lookups (92f9b5c)
- cli: render previously silent top-level errors (c9541a0)

## [v0.1.3] - 2026-02-18

- docs(changelog): let release script generate v0.1.3 entry (83bc7c4)
- docs(changelog): prepare v0.1.3 notes (f893fda)
- feat(batch): add tx ids and transactional history snapshots (6f6ab08)
- feat(reminders): verify reminder writes via backend readback (91a4fac)
- feat(recurrence): enforce strict repeat grammar and canonicalization (0e38309)
- chore: ignore macOS .DS_Store (0de5147)

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
