# acal

`acal` is a Go CLI for querying and managing Apple Calendar with human and agent-friendly output.

This README documents the current `main` branch, including changes being
prepared for v0.3.0. Homebrew installs the latest published release, which can
differ from this development version. Check `acal version` and use the README
at the matching release tag, such as the
[v0.2.1 README](https://github.com/agisilaos/acal/blob/v0.2.1/README.md).
See [releases](https://github.com/agisilaos/acal/releases) for published versions.

The [installed EventKit proof](docs/native-proof.md) is an experimental
development candidate built separately from the Homebrew release. Existing
production commands and history retain their current backend.

Released under the [MIT License](LICENSE). Binary archives include the license.

## Install

```bash
brew tap agisilaos/tap
brew install acal
```

If Homebrew reports that the formula is untrusted, trust acal and retry:

```bash
brew trust --formula agisilaos/tap/acal
brew install acal
```

This trusts only the acal formula so Homebrew can load it. Older Homebrew
versions may not require this step.

Verify:

```bash
acal version
```

## Usage

Use the implemented command set below and the examples section for common flows.

The supported backend is `osascript` (the default). The `eventkit` backend is
not implemented; selecting `--backend eventkit` fails for backend commands.

Run `acal setup --json` or `acal status --json` for health checks and permission
guidance. A ready result does not verify write access. Editing or deleting an
existing event (including reminders and history replay) requires **Full Access**
for the invoking terminal/app under System Settings → Privacy & Security →
Calendars. acal uses EventKit to verify that the target is independent before
writing. Creating an independent event requires this additional access only when
setting a reminder. Permission is checked before mutation; acal does not request
it automatically.

**Recurring-event writes are unsupported in v0.3.0.** Creating a series, adding
recurrence, or modifying/deleting a series or detached occurrence is rejected
before mutation. Use Calendar.app for these operations. Recurring reads retain
their existing limitations. [Full support is planned](https://github.com/agisilaos/acal/issues/35).

Preview parsed event details without writing:

```bash
acal quick-add "tomorrow 10:00 Test @Personal 30m" --dry-run --json
```

This preview does not check that Personal exists or is writable. To verify write
access, choose an existing writable calendar, create a disposable event without
`--dry-run`, confirm it in Calendar, then delete it. That verification changes
Calendar data.

## Implemented

- `doctor`
- `setup`
- `status`
- `version`
- `calendars list`
- `events list`
- `events search`
- `events query` (`--where`, `--sort`, `--order`, `--limit`)
- `events conflicts`
- `events show`
- `events add`
- `events update`
- `events move`
- `events copy`
- `events delete`
- `events remind`
- `events export`
- `events import`
- `events batch`
- `agenda`
- `freebusy`
- `slots`
- `today`
- `week`
- `month`
- `view`
- `quick-add`
- `completion`
- `history list`
- `history undo`
- `history redo`
- `queries save`
- `queries list`
- `queries run`
- `queries delete`

## Output

- `--json` envelope output for agents
- `--jsonl` streaming object-per-line output
- `--plain` stable line-based output
- `--verbose` diagnostics to stderr (resolved command/backend/mode/profile)
- `--timeout` bounds backend calls (default `15s`, set `0` to disable)
- `--fail-on-degraded` fails non-health commands when environment is degraded
- `--no-color` disable ANSI coloring in human-readable errors (also auto-disabled by `NO_COLOR` or `TERM=dumb`)

Plain event records display embedded control characters as visible escapes
(for example, `\n`, `\t`, and `\u001b`) while preserving readable Unicode.
With `--fields`, tabs separate columns and newlines separate records. JSON and
JSONL retain the original field values through standard JSON encoding.
Optional fields display their values (including `false` and empty strings);
unset optional fields display `<nil>`. For example, these previews print
`Changed` and `false`, respectively, without reading or writing an event:

```bash
acal events update review@1 --title Changed --dry-run --plain --fields title
acal events update review@1 --all-day=false --dry-run --plain --fields all_day
```

## Agent usage

Recommended automation patterns:

- Deterministic reads:
  - `acal events query --from today --to +7d --where 'title~standup' --sort start --order asc --json`
- `--schema-version` supports `v1` only (omitted or empty also selects `v1`). Unsupported values exit `2` before backend access; structured errors still use the actual `v1` schema.
- Parser errors honor output flags (including explicit `false`), `ACAL_OUTPUT`, and configuration/profile output settings. If configuration cannot be read, error rendering falls back to environment/flags and preserves the original parser error.
- Unknown subcommands and unexpected positional arguments exit `2` before command execution. Bare command groups still display help.
- Safe writes preview:
  - `acal events add ... --dry-run --json`
  - `acal events batch --file ops.jsonl --dry-run --strict --json`
  - `acal events import --file calendar.ics --calendar Work --dry-run --strict --json`
  - `events.batch` responses include stable `tx_id` and per-row `op_id`.
- Idempotent orchestration:
  - save named filters with `acal queries save <name> ...`
  - execute with `acal queries run <name> --json`
- Rollback guardrails:
  - inspect `acal history list --json`
  - rollback with `acal history undo --json`
  - re-apply with `acal history redo --json`
- Event updates preserve omitted title, location, notes, and URL fields. Supplied values are literal (including `__ACAL_KEEP__`); an empty string clears the field.
- Update timing and history:
  - Ordinary and batch updates calculate duration from the supplied start, or read the existing start when omitted. End-only updates also read the existing start to validate ordering; read failures stop these updates, including previews.
  - Field-only previews and previews with all required timing values supplied do not read the event unless a sequence check is requested.
  - Update history uses a snapshot taken before writing. If that read fails, an ordinary update can proceed without history when neither timing nor sequence validation needs it; batch updates require the snapshot. These reads do not make updates atomic against concurrent external changes, and previously recorded incorrect history is not repaired.
- Quick-add inline durations must be positive: `0m` and `-30m` are errors, not title text. Ordinary numeric title words such as `0` remain literal.
- Local date-times in a daylight-saving gap are rejected instead of silently shifted. Choose a valid wall time or provide an explicit RFC3339 offset; this also applies to quick-add.
- `--if-match-seq` on update, move, delete, and remind checks the supplied nonnegative sequence, including zero. Omitting it disables the check. Delete previews skip the lookup and do not verify the sequence.
- Creation failures and uncertain outcomes:
  - A timed-out or canceled creation, or a native failure after the script launches, may already exist in Calendar. This includes AppleEvent timeouts (`-1712`), connection failures, failed field/alarm writes, and missing creation results. Inspect the selected calendar, title, and start/end times before retrying; repeating the creation can produce duplicates. This applies to quick-add, add, copy, batch add rows, import, and history recreation.
  - JSON/JSONL errors and batch-row metadata include `phase: backend.add_event`, `outcome: unknown`, `verified: false`, and the attempted `calendar`, `title`, `start`, and `end` (RFC3339 values with the resolved offset). Context failures preserve `kind: timeout|canceled` and any timeout `deadline`; native failures use `kind: creation_outcome_unknown`. They make no `applied` assertion.
  - Existing error codes and exit statuses are retained: uncertain native failures use `GENERIC_FAILURE` and exit `1`; context failures use `BACKEND_UNAVAILABLE` and exit `6`, except quick-add and batch errors exit `1` (quick-add uses `GENERIC_FAILURE`). Import errors also retain their confirmed-creation progress metadata.
  - Input validation, recurrence rejection, a confirmed missing calendar or missing reminder permission, and failures to launch the native interpreter do not report an unknown creation outcome. Missing Full Access for reminder creation returns `PERMISSION_DENIED` with `applied: false` before mutation.
  - An uncertain creation does not add an undo entry or advance history replay stacks. Earlier confirmed creations in an import or batch remain applied and recorded. Inspect Calendar before retrying even when history has no entry for the attempted event.
  - Native creation scripts are never automatically retried, including when `ACAL_OSASCRIPT_RETRIES` is configured.
- Update results and uncertain outcomes:
  - Native updates read the complete mutable event fields directly from the object changed by Calendar, preserving notes, URL, and whitespace. They do not use the occurrence cache to construct the result. The returned occurrence ID follows the resulting start time; undo/redo follows returned IDs after moves.
  - A completed write without a verified result exits `1` with `UPDATE_APPLIED_UNVERIFIED` and metadata `applied: true`, `verified: false`. Transport failures or timeouts with uncertain completion exit `1` with `UPDATE_OUTCOME_UNKNOWN`, `verified: false`, and no `applied` assertion. Batch rows expose the corresponding `meta.kind` (`update_applied_unverified` or `update_outcome_unknown`) and inspection hint.
  - Inspect Calendar before retrying an uncertain update, move, reminder, batch row, or undo/redo. These outcomes do not append snapshots or advance history/redo stacks, even though Calendar may have changed. Native update scripts are never automatically retried, including when `ACAL_OSASCRIPT_RETRIES` is configured.
  - Recurring writes are rejected before mutation. Native date setters still preserve absolute instants; ambiguous DST-fold dates produce an unverified outcome.
  - Pre-write rejection includes `meta.kind: write_rejected`, `meta.applied: false`, and a reason. `UNSUPPORTED_OPERATION` exits `2` for recurrence or unclassified historical recreation; `PERMISSION_DENIED` exits `6` for missing Full Access; unresolved/ambiguous native identity exits `6` with `BACKEND_UNAVAILABLE`. These are not uncertain-write outcomes. A rejected action leaves history and redo unchanged. For batch commands this applies to the rejected row; earlier successful rows remain applied.
  - Automated checks use fixtures/stubs and a Calendar-free AppleScript serialization check. Native Calendar round-trip guarantees require a separately authorized disposable-calendar integration run.
- Reminder offsets must be exact whole minutes; fractional-minute values (such as `30s` or `90s`) are rejected before Calendar reads or writes. `events remind --at` requires a nonzero duration and treats either sign as before the event. Native writes and history replay preserve signed whole minutes, including zero for an alarm at the event start.
- Reminder previews (`acal events remind <id> --at -15m --dry-run --json` or `acal events remind <id> --clear --dry-run --json`) read the event but do not update its display alarm or write history/redo. Preview data includes `dry_run: true` in JSON, JSONL, and plain output; JSON metadata also includes `dry_run: true` and `verified: false`. Clear requests include `meta.clear_requested: true`. The legacy v1 `meta.cleared: true` field is retained for compatibility and identifies the requested operation, not proof of completion; only `meta.verified: true` confirms a read-back-verified clear.
- Reminder writes are read-back verified:
  - `acal events remind <id> --at -15m --json` verifies backend reminder state after update.

Exit codes:

- `0`: success
- `1`: runtime/processing failure
- `2`: invalid usage, validation failure, or an unsupported operation
- `4`: resource not found
- `6`: backend unavailable or required Calendar access missing
- `7`: concurrency conflict (sequence mismatch)

Parser errors leave stdout empty and write diagnostics to stderr. With `--json` or
`--jsonl`, the diagnostic is a JSON error envelope with code `INVALID_USAGE`.
For example, `acal events show --json` (missing event ID) and
`acal history list --limit abc --json` both exit `2` before accessing Calendar.

Notes:
- `doctor`, `status`, `status explain`, and `setup` exit `0` when `ready=true` and `6` when required checks fail or are missing, in both plain and JSON output. Degraded environments can still be `ready=true` when core automation checks pass, even if the backend reports an error.
- After checks run, each health command prints one health report to stdout. When not ready, it also prints one stderr diagnostic unless a backend error was reported by `doctor` or `status explain`; those commands keep stderr empty in that case. Not-ready `status` and `setup` results with a backend error include remediation guidance in their stderr diagnostic.
- `status` and `doctor` include `degraded_reason_codes` for machine-actionable remediation.
- `status explain` prints a concise health explanation and remediation steps.

## Config and precedence

Supported precedence: `flags > env > project config > user config > defaults`

- User config: `~/.config/acal/config.toml` (or `$XDG_CONFIG_HOME/acal/config.toml`)
- Project config: `./.acal.toml`
- Env vars:
  - `ACAL_PROFILE`
  - `ACAL_BACKEND`
  - `ACAL_TIMEZONE`
  - `ACAL_TIMEOUT` (e.g. `15s`, `1m`, `0`)
  - `ACAL_FAIL_ON_DEGRADED` (`true|false`)
  - `ACAL_OUTPUT` (`json|jsonl|plain`)
  - `ACAL_FIELDS`
  - `ACAL_NO_INPUT`

Timezone values from `--tz`, `ACAL_TIMEZONE`, or config `tz` are validated after
precedence is applied. An invalid effective value (for example, `Europe/Berln`)
returns usage exit code 2 and names the value before any backend access. An
omitted or empty effective timezone uses the system local timezone; `--tz ''`
explicitly selects that default over an inherited timezone.

Preview an event in a named timezone without writing to Calendar:

```bash
acal events add --calendar Work --title Review --start 2026-10-01T09:00 --duration 30m --tz Europe/Berlin --dry-run --json
```

Missing optional user/project files are allowed. A file selected explicitly with
`--config PATH` or `ACAL_CONFIG` must exist. Existing files that cannot be read or
parsed as TOML cause a usage error (exit 2), even if flags override their settings.
`--config` takes precedence over `ACAL_CONFIG`. An additional config file overlays
the project config, below environment variables and flags; selecting the user or
project path already loaded does not change that layer's position.

Invalid effective `timeout` and `output` values, or environment booleans
(`ACAL_FAIL_ON_DEGRADED`, `ACAL_NO_INPUT`), report their source and accepted format.
Higher-priority values can override invalid lower-priority values after TOML
parsing. An enabled `--json`, `--jsonl`, or `--plain` overrides `ACAL_OUTPUT`;
a false output flag alone does not make an invalid output setting valid.
Empty settings retain their existing unset behavior. Unknown TOML keys are
ignored. The selected profile overlays each file's base settings; an absent
profile uses those base settings, and unselected profiles are not value-validated.

These read-only examples list saved queries without accessing Calendar:

```bash
acal queries list --config ./runtime.toml --json
ACAL_OUTPUT=json acal queries list
ACAL_OUTPUT=jsno acal queries list # exits 2 and names ACAL_OUTPUT
ACAL_OUTPUT=jsno acal queries list --json # flag overrides the invalid value
```

For the config example, create `runtime.toml` containing:

```toml
timeout = "15s"
output = "plain"
```

## Build

```bash
go build ./cmd/acal
```

Examples use `acal` on `PATH`, as installed by Homebrew. When using the
binary built in this checkout, replace `acal` with `./acal`.

## Testing

```bash
make verify
go test ./...
go test ./internal/backend -bench ListEventsViaSQLite -run '^$' -benchmem
make docs-check
```

## Release

Ask an agent to prepare the changelog from commit and PR evidence, review and commit it, then run:

```bash
make changelog-context VERSION=vX.Y.Z
make release-check VERSION=vX.Y.Z
make release-dry-run VERSION=vX.Y.Z
make release VERSION=vX.Y.Z
```

Every new changelog bullet links to its pull request or direct commit. The approved changelog section becomes the GitHub Release notes. The dry run builds both macOS archives with their license and checksums and syntax-checks the Homebrew formula without remote writes. Publication validates the selected tap branch before creating a tag.

See `RELEASING.md` for the full runbook. Release scripts are `scripts/changelog-context.sh`, `scripts/release-check.sh`, and `scripts/release.sh`.

For an interrupted publication, follow the [release recovery runbook](docs/release-recovery.md) with the retained original artifacts.

## Examples

Relative dates (`today`, `tomorrow`, `yesterday`, and `+Nd`/`-Nd`) use local
calendar days in the selected `--tz` timezone (the system timezone by default).
They resolve to midnight even across daylight-saving changes. Quick-add all-day
events end exclusively at the next local midnight, so they can span 23 or 25
hours. Timed durations such as `2h` always mean elapsed time.

Date-only agenda ranges and event-filter end dates include the day's final
second, `23:59:59`. An explicit midnight filter end is expanded the same way.
An agenda `--day` timestamp keeps its time of day and ends one calendar day later,
minus one second.

Quick-add plain output (both `quick-add` and `events quick-add`) defaults to
`id`, `start`, `end`, `calendar`, `title`, with `dry-run` as the preview ID.
Use `--fields title,start` to select columns in that order. Start and end use
RFC3339 timestamps; calendar uses the created event's name, falling back to its
ID. Control characters in plain cells are escaped, including tabs, newlines,
and terminal escape characters. The same escaping protects plain errors, history,
health reports, and verbose diagnostics. Interactive ICS export escapes controls
inside records, including exports to terminal device paths. Use `--out <file>`
with a regular file or redirect `--plain` stdout to preserve serialized ICS bytes. JSON and JSONL retain their full payloads.

```bash
acal doctor --json
acal setup --json
acal status --json
acal version
acal today --json
acal freebusy --from today --to +7d --json
acal slots --from tomorrow --to +3d --between 09:00-17:00 --duration 45m --json
acal today --summary --plain --fields date,total,all_day,timed
acal week --of today --week-start monday --plain
acal week --summary --json
acal month --month 2026-02 --json
acal view month --month 2026-02 --summary --plain --fields date,total
acal quick-add "tomorrow 10:00 Standup @Work 30m" --dry-run --json
acal quick-add "2026-10-01 09:00 Review @Work 30m" --dry-run --plain --fields title,start --tz UTC
acal history list --json
acal history list --json --limit 10 --offset 10
acal history undo --dry-run --json
acal history redo --dry-run --json
acal queries save next7 --from today --to +7d --where 'title~standup' --limit 10
acal queries run next7 --json
acal events quick-add "2026-02-18 09:15 Deep Work @Personal 45m"
acal events list --from today --to +7d --json
acal events list --from today --to +7d --verbose --json
acal events query --from today --to +14d --where 'title~sleep' --sort start --order asc --plain --fields id,title,start,end
acal events conflicts --from today --to +14d --json
acal events add --calendar Personal --title "1:1" --start 2026-02-10T10:00 --duration 30m
acal events add --calendar Work --title "Standup" --start 2026-02-20T09:00 --duration 30m --repeat 'daily*5' --dry-run --json
acal events update <event-id> --location "Room 4A" --scope auto --if-match-seq 1
acal events update <event-id> --repeat 'weekly:mon,wed*6' --dry-run --json
acal events move <event-id> --by 30m --scope auto
acal events move <event-id> --to 2026-02-20T14:00 --duration 45m --dry-run --json
acal events copy <event-id> --to 2026-02-21T09:00 --duration 30m --calendar Personal
acal events remind <event-id> --at -15m --json
acal events export --from today --to +14d --out calendar.ics
acal events import --file ./calendar.ics --calendar Work --dry-run --json
acal events batch --file ./ops.jsonl --dry-run --json
acal events delete <event-id> --confirm <event-id> --scope auto --no-input
acal events delete <event-id>   # interactive TTY confirmation prompt
```

### Batch JSONL schema

`events batch` reads one JSON object per nonblank line. Only these keys are
recognized; string fields accept JSON strings and `all_day` accepts a boolean:

| Operation (`op`) | Required fields | Optional fields used by the operation |
| --- | --- | --- |
| `add` | `calendar`, `title`, `start`, and either `end` or `duration` | `location`, `notes`, `url`, `all_day` |
| `update` | `id` | `title`, `start`, `end`, `duration`, `location`, `notes`, `url`, `all_day`, `scope` |
| `delete` | `id` | `scope` |

`start` and `end` use the CLI datetime syntax and `--tz`; `duration` is a positive
Go duration such as `30m` or `1h`. If both `end` and `duration` are supplied, `end`
takes precedence. Update duration uses the supplied start or reads the existing
start, including in dry runs. `scope` accepts `auto` (default), `this`, `future`, or `series`.
Optional null values act like omitted values. Known keys unused by an operation
retain their existing ignored behavior.

Unknown keys (including `repeat`, misspellings, and producer metadata) now fail
that row before it executes, with the field name in the row error and exit status
1. Producers that previously attached extra keys must remove them or keep their
metadata outside the batch input. Do not remove `repeat` expecting recurrence to
survive: batch does not create or change recurrence rules; use the dedicated
`events add` or `events update` command for recurrence.

Rows execute in order. By default, processing continues after errors;
`--strict` or `--continue-on-error=false` stops at the first failed row. This is
not whole-file preflight or a transaction: earlier successful writes remain.
Preview the file first with `--dry-run --strict --json`.

For example, save these rows as `ops.jsonl` (replace the sample IDs before writing):

```jsonl
{"op":"add","calendar":"Work","title":"Batch","start":"2026-10-01T09:00","duration":"30m","location":"Room 4A","notes":"Planning","url":"https://example.com","all_day":false}
{"op":"update","id":"sample-event-id","title":"Revised","scope":"this"}
{"op":"delete","id":"sample-event-id","scope":"this"}
```

```bash
acal events batch --file ./ops.jsonl --dry-run --strict --json
```

ICS import supports independent events only. VEVENT entries containing `RRULE`,
`RDATE`, `EXDATE`, or `RECURRENCE-ID` are skipped with warnings rather than
flattened into one-off appointments. `--strict` rejects a file with any parser
warnings before creating any events, including otherwise valid entries. Use
`--dry-run --json` to inspect importable events and warnings.

Imports write events one at a time and record each confirmed creation in undo history, clearing redo. On failure, earlier creations remain: JSON/JSONL error metadata reports `created_ids`, `count`, and the one-based importable `failed_item`; plain errors include the same progress in the hint. A history recording failure stops the import after the completed creation. Inspect Calendar and history before retrying, since the last attempted write may also have completed. Undo recorded entries individually or delete confirmed IDs; retrying the whole file can create duplicates.

ICS export writes the occurrences returned for `--from`/`--to` (and `--limit`,
when set) as separate VEVENT entries. It does not reconstruct recurrence rules
or exceptions, so exporting and importing is not a recurrence-preserving round trip. All-day dates use the selected `--tz` (system timezone by default), with an exclusive end date; timed events are exported as UTC instants. Use the same timezone when importing all-day dates to preserve that calendar-day view.

Each `--where` argument is one literal `field operator value` clause. Repeat the
flag to combine clauses with AND. Commas and embedded double quotes are passed
through flag parsing, without CSV decoding. Shell-quote the whole clause:

```bash
acal events query --from today --to +7d --where 'title~a,b' --where 'calendar==Work' --json
acal queries save comma-titles --from today --to +7d --where 'title~"a,b"' --where 'calendar==Work'
acal queries run comma-titles --json
```

Migration: replace legacy comma lists such as `--where 'title~walk,calendar==Work'`
with `--where 'title~walk' --where 'calendar==Work'`. Commas never separate clauses
now. Existing saved `wheres` arrays already contain separate clauses and need no
conversion; each element remains one clause.

String fields are `title`, `calendar` (alias `calendar_name`), `calendar_id`,
`location`, `notes`, and `id`. They support case-insensitive equality (`==`),
inequality (`!=`), and substring matching (`~`). Time fields `start` and `end`
support `==`, `!=`, `>`, `>=`, `<`, and `<=` with RFC3339 values, for example
`--where 'start>=2026-02-20T09:00:00Z'`.

The existing predicate grammar is unchanged: field names ignore case; whitespace
around the clause, field, and value is trimmed; leading/trailing double quotes
are stripped from the value. Embedded quotes remain literal, with no escape
processing. Blank clauses are ignored and empty values are invalid. Operators
are searched in precedence order `==`, `!=`, `~`, `>=`, `<=`, `>`, `<`, across the
whole clause. Consequently values containing a higher-precedence operator are
not supported (for example, `title~a==b`); quoting does not escape operators.

Sorting accepts `start` (default), `end`, `title`, `updated_at`, or `calendar`;
order accepts `asc` (default) or `desc`. Both accept case variants. Unsupported
values, including explicitly empty direct-query flags, return invalid usage
(exit 2) before listing events. Older saved presets with empty or omitted sort
or order retain the corresponding default.

Query execution validates every `--where` clause before listing events, including
when the result would be empty or an earlier clause would exclude every event.
Setup and date-range errors retain precedence. Clause syntax is checked first,
then fields, operators, and values are validated in clause order. Saved queries
keep their raw clauses and sorting options and are validated when run, not when
saved. Sorting is validated after predicates, before listing events.

`events query` and `queries run` apply predicates and sorting before the result limit.
A positive limit returns at most that many matches; zero or a negative limit returns all matches.
These commands fetch the complete selected date/calendar range, so a small result limit does not reduce scan work or memory use. Narrow the range or calendars for large datasets.
Equal sort keys retain their fetched order in either direction; tie order can differ between backends.

## Docs

- CLI roadmap and expansion plan: `docs/cli-expansion-roadmap.md`
- Release notes history: `CHANGELOG.md`

## Notes

Ordinary event lists and identity lookup retain inclusive start-in-range selection.
`freebusy`, `slots`, and `events conflicts` instead retrieve events overlapping the
resolved range: an event must start before `--to` and end after `--from`. An event
ending exactly at the start or starting exactly at the end does not overlap.
Zero-duration and inverted intervals are excluded before the availability scan
limit; ordinary listing still includes them.
Equal non-midnight bounds produce empty availability. Existing date-only and
midnight end expansion still applies. Busy blocks and conflict overlap endpoints
and minutes are clipped to that resolved range; event IDs remain unchanged.
All-day events still count toward scanned events but affect availability only
with `--include-all-day`. A scan limit can omit busy events and conflicts.

`slots` uses the same resolved range for fetching events and finding gaps. A date-only `--to` includes its final day; explicit midnight ends receive the same expansion as event filters, while non-midnight timestamps clip the range exactly. `--limit` caps events scanned, not slots returned.

- `events conflicts` caps output at 1,000 pairs by default; `--max-conflicts` accepts 1–10,000. JSON reports the cap in `meta.max_conflicts`, the returned count in `meta.count`, and omitted pairs through `meta.truncated` and a warning. Plain/JSONL warns on stderr. Narrow the date range or calendars when truncated. The separate `--limit` flag limits input events; truncation metadata only describes pairs among those events.
- Event listing uses the local Calendar SQLite occurrence cache for reliable recurring-instance reads.
- Event lookup requires an exact occurrence ID (`<uid>@<integer Cocoa start>`). It searches around the encoded start, including occurrences outside the former three-year past/future window. UID-only or malformed IDs return `event not found`; lookup does not refresh the occurrence cache.
- SQLite reads run in-process via `database/sql` (`modernc.org/sqlite`) with read-only access and per-path connection reuse to reduce subprocess/open overhead. SQLite detects externally committed changes, including WAL updates; access or query errors fall back to AppleScript, while cancellation and timeout errors are returned.
- Event fields use AppleScript against Calendar.app; display-alarm replacement uses an EventKit bridge inside the same native script. It replaces only display alarms, preserves the event and other alarm types, and verifies the saved alarm. Matching requires a unique UID/start/calendar-name combination; ambiguity fails instead of choosing a different event. Calendar names and event IDs are passed after an explicit option terminator so leading hyphens remain literal data.
- Immediately after writes, Calendar's publication of changes to its occurrence cache can lag briefly; SQLite change detection does not force that refresh.
- `status` reports readiness/degraded state plus active backend/profile/tz/output mode for automation diagnostics.
- `status`/`doctor` include machine-friendly `degraded_reason_codes` metadata when checks degrade.
- `--verbose` includes per-command backend timing diagnostics and `meta.timings` in JSON responses.
- Timeout/cancel errors now include backend phase context (for example `backend.list_events timed out...`) to make hang diagnosis faster.
- JSON error payloads include structured timeout/cancel metadata under `meta` (`phase`, `kind`, `deadline`) and map these failures to `BACKEND_UNAVAILABLE` for consistent automation handling.
- Optional transient AppleScript retry controls for reads (off by default; creation, update, and deletion scripts never retry):
  - `ACAL_OSASCRIPT_RETRIES` (integer retries; default `0`)
  - `ACAL_OSASCRIPT_RETRY_BACKOFF` (duration; default `200ms`)
- Persistence files live in `$XDG_CONFIG_HOME/acal/`, or `$HOME/.config/acal/` when `XDG_CONFIG_HOME` is unset. `--config` and `ACAL_CONFIG` select an additional configuration layer; they do not relocate history, redo, or saved queries:
  - `config.toml`: runtime defaults/profiles.
  - `history.jsonl`: mutable undo stack. Writes append entries; undo removes entries by rewriting the stack. It is not an append-only audit log.
    - JSONL fields: `{"at","type","tx_id","op_id","event_id","prev","next","created","deleted","reminder_before","reminder_after"}`; operation-specific fields are omitted when unused.
    - New `events remind` writes use `type: "reminder"`, a nonblank `event_id`, and required `reminder_before` / `reminder_after` objects instead of ordinary event snapshots. Each object has an optional `offset_ns` (signed integer nanoseconds): null or omitted means known-none; zero is an actual zero-offset alarm. A missing/null snapshot object is invalid, not a clear instruction. Readers reject invalid reminder payloads in parseable JSON rows; syntactically corrupt JSON lines retain the legacy skip behavior.
  - `redo.jsonl`: redo stack populated by `history undo`.
    - JSONL schema: same as `history.jsonl`.
  - Successful `quick-add` and `events quick-add` writes record one undo entry and clear redo before rendering, in plain (including interactive), JSON, and JSONL output. Dry runs and failed writes leave both stacks unchanged. History append failures do not turn a completed Calendar write into a command failure.
  - `history list`, `history undo`, and `history redo` expose the same entry fields in JSON / JSONL; the JSON envelope remains `schema_version: "v1"`.
  - Reminder undo/redo changes only the display-alarm offset and verifies it by reading it back. `events remind` reads the prior offset before mutation and aborts if that read fails. An update or verification failure creates no new reminder history; failed undo/redo verification leaves the stacks unchanged, although Calendar may already have changed. History write failures retain their existing handling.
  - Reminder snapshots cover only the first display alarm (or none). Set/clear and replay replace all display alarms; additional display alarms cannot be restored. Other alarm types are not captured or changed by these operations.
  - New history entries record `independent: true` after successful guarded writes. Legacy entries remain readable, but undo-delete and redo-add cannot recreate a snapshot without that classification: the original recurrence structure is unknown, so both stacks remain unchanged. Successful guarded replay of a live target establishes independent status for subsequent replay. Older generic reminder updates have no recoverable alarm snapshot and cannot be repaired retrospectively; no notes-marker migration is performed.
  - Downgrade limitation: older acal binaries reject reminder entries during undo/redo, and can discard their snapshot fields when rewriting a stack for another operation. Older binaries also drop the new independence classification when rewriting entries, which can block later recreation in v0.3.0. Do not use an older binary against these history files if recovery is needed.
  - `queries.json`: saved query aliases. Concurrent saves and deletes are serialized using `queries.json.lock`; updates replace the store atomically so readers see a complete snapshot. A missing, empty, or JSON `null` store is treated as empty; malformed JSON is reported without overwriting it.
    - JSON schema: `{ "<name>": {"name","from","to","calendars","wheres","sort","order","limit"} }`
- History and redo contain full event snapshots. On macOS, accessing them restricts the `acal` config directory to `0700` and each accessed snapshot file to `0600`, including existing storage and history dry runs. Shared parent directories are unchanged.
- Delete safety model:
  - interactive TTY: prompts for exact event ID unless `--force` or `--confirm` is supplied.
  - non-interactive or `--no-input`: requires `--force` or exact `--confirm <event-id>`.
- Write scope flags remain accepted for compatibility, but no scope bypasses the recurring-event restriction. `auto` resolves an occurrence-style ID to `this`; `this` and `future` require an occurrence-style ID. Independent events remain writable after classification.
- Recurring-series anchors, generated occurrences, and detached occurrences are rejected. Missing or ambiguous native classification also stops before mutation. The check runs again inside the write script; it is not an atomic transaction against concurrent external Calendar edits.
- Repeat rule grammar (`events add|update --repeat`, preview only; writes are unsupported):
  - `daily*<count>`
  - `weekly:<day[,day...]>*<count>` where day is `mon|tue|wed|thu|fri|sat|sun`
  - `monthly*<count>`
  - `yearly*<count>`
  - Count must be `1..366`. A dry run previews input; it does not authorize or certify a later write. Delete/history previews may skip native target checks.
- History pagination:
  - `history list --limit <n>` returns at most `<n>` most-recent entries (default `10`; zero or negative limits also use `10`).
  - `history list --offset <n>` skips `<n>` most-recent entries before applying `--limit`. Negative offsets are usage errors (exit `2`).
  - Pagination metadata reports the effective limit and offset; offsets beyond the stored history return an empty page.

### Calendar names containing commas

Read filters and saved queries accept repeated `--calendar` flags and CSV lists.
For a name containing a comma, use its ID from `acal calendars list --json`, or
preserve CSV double quotes inside shell single quotes:

```bash
acal events list --calendar '"Holiday, Family"' --json
acal events list --calendar Work --calendar '"Holiday, Family"' --json
```

`--calendar 'Holiday, Family'` instead selects two names (`Holiday` and ` Family`).
Single-calendar creation flags such as add/import/quick-add take a literal calendar
name, not an ID; use `--calendar 'Holiday, Family'` there, without embedded CSV
quotes. Copy destinations and the batch add `calendar` field also use names.
The native backend selects the first calendar matching that name, so duplicate
calendar names are ambiguous for creation.

### ICS import date support

ICS import accepts UTC date-times (`20260220T090000Z`), floating date-times,
and `VALUE=DATE` all-day dates. `VALUE=DATE-TIME` is also supported. Floating
values and all-day dates use `--tz`; date-times with `TZID` use the installed
IANA timezone database, preserving identifier casing (for example,
`DTSTART;TZID=America/New_York:20260220T090000`). Quoted TZID values are accepted.
Winter and summer offsets follow the named zone's rules.

Embedded `VTIMEZONE` definitions are not interpreted: recognized IANA identifiers
use the system rules, and unknown/custom identifiers cause that VEVENT to be
skipped with a warning. Unsupported VALUE types, invalid dates, and TZID on UTC
or DATE values also cause warning/skip behavior. `--strict` rejects a file with
parser warnings before creating any events, including otherwise valid entries.
Corrected timezone handling affects new imports only; existing imported events
are not repaired.
