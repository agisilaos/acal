# Architecture

acal is a local macOS CLI running with the invoking user's filesystem and Calendar
permissions. It has no server, service account, or network API. The implemented
backend is `osascript`, with an EventKit bridge for display-alarm changes; the
standalone `eventkit` backend selection currently returns an error.
The [README](../README.md) owns command syntax and output contracts; this document
maps the implementation and the boundaries that changes need to preserve.

## Ownership and data flow

| Owner | Responsibility | Main sources |
| --- | --- | --- |
| Entry point | Build information, command execution, process exit | `cmd/acal/main.go` |
| Application | Cobra commands, effective options, validation, timeout contexts, planning, mutation orchestration | `internal/app/root.go`, `config.go`, `commands_*.go` |
| Local state | Undo/redo snapshots and replay; saved-query storage | `internal/app/history.go`, `commands_history.go`, `commands_queries.go` |
| Backend interface | Typed read filters, mutation inputs, recurrence scope | `internal/backend/backend.go` |
| Native adapter | Calendar SQLite reads, AppleScript fallback and mutations, native result decoding | `internal/backend/osascript*.go`, `update_result.go` |
| Public data and output | Event/envelope types, schema version, JSON/JSONL and safe plain rendering | `internal/contract/types.go`, `internal/output/printer.go` |
| Time parsing | User date/time and duration parsing | `internal/timeparse/timeparse.go` |
| Repository tooling | Documentation/help contracts, builds, release checks and publication | `Makefile`, `scripts/`, `.github/workflows/release-check.yml` |

Commands resolve configuration and validate input before dispatching typed backend
operations. The app owns snapshots and user-facing outcomes; the backend owns
native targeting and result verification. Unix/native date conversion uses Foundation
rather than arithmetic on a locale-parsed epoch; occurrence selection compares
native dates. Repeated local times during a DST fold remain ambiguous in AppleScript. Returned records flow through the output
package, with dedicated renderers for health, history, and ICS.

## Reads and writes

Event reads use Calendar's occurrence-cache SQLite database in read-only mode.
Connections are reused, but the database remains mutable and SQLite owns change
detection. Non-context read failures can fall back to AppleScript; cancellation
and deadline failures do not. Calendar can publish cache changes after a write
returns, so read-only connection freshness does not imply immediate native-cache
freshness. Planning uses overlap selection; ordinary listing retains start-range
selection. Exact occurrence IDs couple a UID with its Cocoa-epoch start.

Calendar enumeration and mutations use fixed AppleScript source with values passed
as arguments. Both launchers put `--` between trusted interpreter options/source
and data. Tabular reads request raw interpreter output and preserve empty trailing
cells when splitting rows. This boundary must hold for every caller, including batch input and
history replay. SQL literals and LIKE patterns have their own escaping in the read
adapter; neither boundary should rely on a caller sanitizing event text.

Display-alarm replacement uses EventKit from the native script because Calendar's
AppleScript delete handler fails for alarms. It requires full Calendar access for
the invoking app, checked before any event mutation. The bridge resolves exactly
one event using UID, start instant, and calendar name; it fails on ambiguity.
It changes only display alarms, saves the existing event using the requested
occurrence/future span, and verifies alarm values after refresh. No new helper
executable, runtime compiler, or automatic permission request is needed.

Native updates read results from the object changed by Calendar. They distinguish
verified results, applied-but-unverified results, and unknown completion. Updates
are never automatically retried. Representative series results do not verify every
occurrence. Sequence checks and pre-write snapshots are separate reads, not atomic
transactions against external Calendar changes. Import and batch apply sequentially;
earlier successful operations remain when later operations fail. A dry run only
controls that invocation and does not bind a later apply to the same file contents.

Creation selects the first calendar with the supplied name. Read filters support
IDs and names, but creation does not resolve calendar IDs. Keep this distinction
visible in help and docs until native creation gains a different targeting model.

## Configuration and persistent state

Effective options combine defaults, user config, project-local `.acal.toml`, an
additional explicitly selected config, environment, and flags. Selected profiles
overlay each file. Project configuration is loaded automatically from the working
directory and is therefore a lower-trust input when running in another project.

History, redo, and saved queries derive their directory independently from
`defaultUserConfigPath`: `$XDG_CONFIG_HOME/acal`, otherwise `$HOME/.config/acal`.
Changing `--config`, `ACAL_CONFIG`, or a profile does not isolate these stores.
History and redo contain full event snapshots and are mutable replay stacks;
access restricts the acal directory to `0700` and accessed snapshot files to `0600`.
This protects ordinary local-account separation, not against an actor controlling
the same account or storage ancestors.

Saved-query writers lock `queries.json.lock` and replace `queries.json` atomically.
That serialization belongs to the query store; it is not a general transaction or
locking guarantee for history, Calendar, or batch operations.

## Output and validation boundaries

Calendar text, imported values, project configuration, and diagnostics remain data
at display boundaries. Plain cells, errors, and custom renderers share control
escaping; trusted separators and application-owned color sequences stay outside
that transformation. ICS checks the actual destination descriptor, including
explicit terminal device paths. Interactive exports escape controls inside records;
regular files and pipes preserve serialized content. JSON/JSONL retain semantic
values through JSON encoding.

Tests use fake backends, owned SQLite fixtures, and harmless native AppleScript
handlers. These validate application contracts and interpreter argument isolation
without changing Calendar. They do not establish native Calendar round-trip or
macOS permission guarantees; those require a disposable-calendar integration run.
`make test vet fmt-check docs-check` covers the routine checks, and
`make release-check-ci` adds module metadata and a version-stamped build.

CI validates changes but does not publish releases. Publication uses the
maintainer's Git/`gh` credentials and target configuration. See
[Releasing](../RELEASING.md) for the human review and repository-target requirements.

An opt-in native alarm regression creates and cleans up uniquely named calendars:

```bash
ACAL_CALENDAR_INTEGRATION=1 go test ./internal/backend -run '^TestNativeReminder' -count=1 -v
```

It requires Calendar Automation permission and full Calendar access. It covers
clear/replacement, signed and zero offsets, event identity/fields, and recurrence
save spans from the first occurrence. It does not qualify targeting later
generated occurrences; Calendar can report those as not found before the alarm
bridge runs. Routine CI skips these live mutations; in-memory EventKit alarm tests run
on macOS without saving to a calendar.
