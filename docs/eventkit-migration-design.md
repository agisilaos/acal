# EventKit migration: agreed direction and proposed first slice

Design tracking: [#38](https://github.com/agisilaos/acal/issues/38), under
[#36](https://github.com/agisilaos/acal/issues/36). This document describes proposed
work, not the currently implemented architecture. The user authorized the first slice through the ship-change workflow.
The [experimental public route](native-proof.md) keeps production history separate.

## Agreed direction

- Keep Go for the CLI, configuration, output and workflow orchestration.
- Qualify one precompiled Swift helper for Calendar authorization, native targeting,
  reads, writes and authoritative local readback. No runtime compiler for users.
- Target macOS 14+, arm64 and amd64, local and iCloud calendars, and logged-in
  terminals/desktop agents. These are qualification targets, not verified coverage.
- Preserve v0.3.0's recurring-write restriction. Migration belongs to the next
  milestone; full recurrence remains in #35.
- Use opaque local identities, conservative conflict detection, bounded recovery,
  durable operation records and separate versioned history.
- Preserve useful commands and output where truthful; document incompatible IDs,
  sequence/revision fields, date bounds, reminder input and legacy storage explicitly.

Decisions: [ADR 0002](adr/0002-qualify-native-migration-after-v0.3.0.md),
[ADR 0003](adr/0003-native-ownership-and-safe-recovery.md),
[ADR 0004](adr/0004-explicit-calendar-semantics.md), and
[ADR 0005](adr/0005-homebrew-without-notarization.md) for Homebrew without notarization.

## First slice: prove the installed native path

Produce an experimental installable candidate retaining the Go entry point and
public command route. Package a Swift helper with versioned JSON requests/results,
separate diagnostic stderr, request/operation IDs and explicit outcome states.
Use an absolute install-relative helper path and reject incompatible protocol
versions. Do not load a helper from the invoking project's working directory.

Start with a private signed app bundle containing the helper and required usage
metadata, installed beside the CLI's private resources. Exact placement and launch
mechanism are prototype variables: neither bundling nor signing establishes TCC
attribution. Test the actual package from the intended terminal and desktop agent.
Compare two successive signed package builds installed through the intended path
to observe permission persistence. Retain Homebrew and archive installation; do
not publish either during the proof. Do not replace the user's working installation.

Implement only the native operations needed to:

1. Report authorization without prompting, request Full Calendar Access only
   through explicit setup, and diagnose denied/restricted access noninteractively.
2. List calendars with authoritative local IDs and writability; list bounded
   occurrences and read a uniquely selected independent event.
3. Create, read, edit and delete a disposable independent event through installed
   acal commands, returning immediately usable post-write IDs.
4. Set, replace and clear a display alarm, verifying native values after refresh.
5. Reject ambiguous targets, unsupported recurrence, read-only targets and known
   permission failures before mutation; expose uncertain completion accurately.

Use minimal durable intent/result records for experimental writes, with an isolated
state root and per-store writer serialization. Do not claim these records provide
production undo/redo or crash reconciliation. Complete those contracts in the
production migration slice before general write cutover. Killing the helper can
bound waiting but cannot prove a write did not happen; never automatically retry.

## Fixture and evidence contract

Use a temporary working directory and XDG configuration/state root: changing only
`--config` does not isolate current history. Bypass inherited project configuration
and record the effective state paths. Do not alter real history or migrate it in
this slice. Avoid capturing private event content in logs or reports.

Use uniquely identified calendars/events created by this work, recording ownership
in a fixture manifest. Execute live operations sequentially. Cleanup resolves exact
owned identities, never title-only matches or broad queries. If completion is
uncertain, retain the manifest and report cleanup status rather than guessing.
Calendar fixture lifecycle may use the native integration harness; event operations
must exercise the installed CLI. No personal event mutations.

Record executable/package versions, installation route, OS/build, architecture,
provider, launch context, signing/notarization status, commands, exit statuses,
redacted output, native readback and cleanup. Authoritative readback means local
EventKit state, not confirmation that iCloud or another device has synchronized.

The current host is macOS 27.2 beta (26B5091g), with Swift from Xcode-beta. An Apple Development
identity was observed, but no Developer ID Application identity is available and
notarization is no longer required for the selected Homebrew path. No supported
stable test environment has been verified.
Local beta tests can inform implementation; they cannot qualify the support matrix.

## Success and stop conditions

The proof is feasible only after installed-package permission handling, independent
CRUD, alarms/readback and upgrade behavior are evidenced on stable macOS in the
claimed contexts/providers. Record exact tested combinations; do not extrapolate a
local-calendar arm64 pass to iCloud, Intel or all macOS versions. Build outputs for
both architectures are not equivalent to live qualification on both.

If stable hardware or provider fixtures are unavailable, preserve the
candidate and report the missing evidence. Do not bypass packaging validation,
silently weaken the scope, or remove the old production implementation on beta-only
evidence. Revisit packaging/integration if installed permission attribution fails.

## Subsequent bounded slices

After feasibility and explicit design approval, track separate child issues for:

1. Native reads/identity, documented output migration and setup capability reporting.
2. Production writes, property qualification, conditional revision checks, durable
   outcome reconciliation, locked history transitions and conflict-safe undo/redo.
3. Safe configuration/history/saved-query migration and cutover. Remove obsolete
   AppleScript/SQLite code, settings, dependencies and docs when replacements pass;
   no automatic or permanent legacy fallback. Any temporary development coexistence
   ends at qualified cutover or abandonment of the candidate.
4. Packaged consumer review using the requested public workflow, including undo/redo.
   Disclose code knowledge; record the initial attempt before assisted recovery.
   Record findings before proposing any fixes as a separate scope.

The first proof is not the final consumer review and does not certify whole-CLI
quality. No merge or release publication without explicit authorization.
