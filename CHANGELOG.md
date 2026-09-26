# Changelog

All notable changes to this project will be documented in this file.

## [v0.3.0] - 2026-09-26

This release focuses on safer Calendar automation, more predictable queries, and clearer failure handling.

### Security

- Keep event text and calendar names out of AppleScript option parsing, escape untrusted terminal controls in plain output and interactive ICS exports, and restrict accessed undo/redo files to the current user. JSON values and noninteractive ICS content remain unchanged. ([native arguments](https://github.com/agisilaos/acal/commit/f366230), [terminal output](https://github.com/agisilaos/acal/commit/776d104), [private history](https://github.com/agisilaos/acal/pull/9))

### Fixed

- Clear and replace display reminders, including undoing back to no reminder, without recreating the event. Preserve other alarm types and verify the saved reminder. ([alarm fix](https://github.com/agisilaos/acal/commit/d35e30e))

- Keep events visible in AppleScript fallback reads when location, notes, or URL fields are empty, and preserve literal quotes and backslashes in returned text. ([fallback reads](https://github.com/agisilaos/acal/commit/3fbe34b))
- Apply query limits after filtering and sorting, preserve literal predicates containing commas or quotes, and observe external Calendar database commits when reusing connections. ([query limits](https://github.com/agisilaos/acal/pull/7), [literal filters](https://github.com/agisilaos/acal/pull/32), [database freshness](https://github.com/agisilaos/acal/pull/18))
- Include events that overlap an availability window even when they start earlier; resolve slot boundaries once and bound conflict output with an explicit truncation warning. ([overlap](https://github.com/agisilaos/acal/pull/23), [slots](https://github.com/agisilaos/acal/pull/10), [conflicts](https://github.com/agisilaos/acal/pull/11))
- Preserve calendar-day ranges across daylight-saving changes, reject nonexistent local times, honor supported ICS timezone parameters, and export all-day dates in the selected timezone. Native Calendar setters now preserve absolute instants instead of shifting them through a local epoch. Existing incorrectly dated events are not repaired. ([calendar days](https://github.com/agisilaos/acal/pull/16), [DST gaps](https://github.com/agisilaos/acal/commit/b89f2d5), [ICS import](https://github.com/agisilaos/acal/pull/22), [all-day export](https://github.com/agisilaos/acal/commit/8f6058c), [native dates](https://github.com/agisilaos/acal/commit/ea3ec39))
- Snapshot updates before mutation, preserve literal field values, and read update results from the event Calendar actually changed. Explicit sequence guards now include zero. ([snapshots](https://github.com/agisilaos/acal/pull/4), [literal values](https://github.com/agisilaos/acal/pull/12), [native results](https://github.com/agisilaos/acal/pull/24), [sequence guards](https://github.com/agisilaos/acal/commit/872b024))
- Record quick-add and successful import operations for undo, report partial import progress, and restore reminder offsets during undo/redo. Concurrent saved-query writes no longer overwrite one another; large history offsets return safely. ([quick-add](https://github.com/agisilaos/acal/pull/17), [import](https://github.com/agisilaos/acal/commit/14134fb), [reminder history](https://github.com/agisilaos/acal/pull/15), [saved queries](https://github.com/agisilaos/acal/commit/2893760), [pagination](https://github.com/agisilaos/acal/pull/19))
- Make parser errors follow the requested output format and exit with usage status 2; align health exits with readiness and make reminder previews explicit. Render optional plain fields as values rather than pointer addresses. ([parser errors](https://github.com/agisilaos/acal/commit/ddf9bad), [usage exits](https://github.com/agisilaos/acal/pull/29), [health](https://github.com/agisilaos/acal/pull/6), [previews](https://github.com/agisilaos/acal/pull/26), [plain fields](https://github.com/agisilaos/acal/pull/25))

### Upgrade notes

- Enable **Full Access** for your terminal/app in System Settings → Privacy & Security → Calendars before changing reminders or replaying reminder history. acal checks access before mutation and does not request permission automatically. ([Calendar access](https://github.com/agisilaos/acal/commit/d35e30e))

- Check scripts that relied on permissive input: invalid effective configuration/timezones, extra positional arguments, unsupported schema versions, unknown batch JSONL fields, nonpositive quick-add durations, and fractional-minute reminders now fail validation. Structured output supports schema `v1` only. ([configuration](https://github.com/agisilaos/acal/pull/33), [timezones](https://github.com/agisilaos/acal/pull/28), [arguments](https://github.com/agisilaos/acal/commit/d88197c), [schema](https://github.com/agisilaos/acal/commit/91fa2c9), [batch](https://github.com/agisilaos/acal/pull/31), [duration](https://github.com/agisilaos/acal/commit/37d91de), [reminders](https://github.com/agisilaos/acal/pull/20))
- Inspect Calendar before retrying `UPDATE_APPLIED_UNVERIFIED` or `UPDATE_OUTCOME_UNKNOWN`: a write may already have happened. These outcomes do not advance undo/redo stacks. Series results represent one changed event, and repeated local times during a DST fold remain ambiguous. ([update outcomes](https://github.com/agisilaos/acal/pull/24))
- Keep older binaries away from history files containing the new reminder snapshots if you need reminder recovery: older versions cannot replay them and may discard their fields when rewriting a stack. Existing historical reminder entries cannot be repaired. ([reminder history](https://github.com/agisilaos/acal/pull/15))
- ICS import skips unsupported recurrence with warnings instead of silently flattening it; `--strict` rejects files with parser warnings before creating events. ICS export/import remains unsuitable for preserving recurrence and exceptions. ([recurrence handling](https://github.com/agisilaos/acal/pull/21))
- Direct-download scripts must use `acal_0.3.0_darwin_arm64.tar.gz` or `acal_0.3.0_darwin_amd64.tar.gz` and `SHA256SUMS`; archive filenames no longer include the leading `v`, and the checksum file no longer has a `.txt` suffix. Homebrew handles the new names. ([release packaging](https://github.com/agisilaos/acal/commit/4fc68a7))
- acal is now MIT licensed; both macOS archives include the license notice. ([license](https://github.com/agisilaos/acal/commit/076108d))

### Known limitations

- Later generated occurrences of a recurring event can return `event not found` when targeted with `--scope future`; the alarm fix does not resolve Calendar occurrence targeting. AppleScript fallback reads can require a longer `--timeout` when Calendar has many calendars. ([documented limitations](https://github.com/agisilaos/acal/commit/d35e30e), [recurrence follow-up](https://github.com/agisilaos/acal/issues/34))

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
