# acal

`acal` is a Go CLI for querying and managing Apple Calendar with human and agent-friendly output.

## Install

```bash
brew tap agisilaos/tap
brew install acal
```

Verify:

```bash
acal version
```

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

## Output modes

- `--json` envelope output for agents
- `--jsonl` streaming object-per-line output
- `--plain` stable line-based output
- `--verbose` diagnostics to stderr (resolved command/backend/mode/profile)
- `--timeout` bounds backend calls (default `15s`, set `0` to disable)
- `--fail-on-degraded` fails non-health commands when environment is degraded
- `--no-color` disable ANSI coloring in human-readable errors (also auto-disabled by `NO_COLOR` or `TERM=dumb`)

## Agent usage

Recommended automation patterns:

- Deterministic reads:
  - `acal events query --from today --to +7d --where 'title~standup' --sort start --order asc --json`
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
- Reminder writes are read-back verified:
  - `acal events remind <id> --at -15m --json` verifies backend reminder state after update.

Exit codes:

- `0`: success
- `1`: runtime/processing failure
- `2`: invalid usage or validation failure
- `4`: resource not found
- `6`: backend unavailable
- `7`: concurrency conflict (sequence mismatch)

Notes:
- `doctor` and `status` share readiness semantics. Degraded environments can still be `ready=true` when core automation checks pass.
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

## Build

```bash
go build ./cmd/acal
```

## Testing

```bash
go test ./...
go test ./internal/backend -bench ListEventsViaSQLite -run '^$' -benchmem
make docs-check
```

## Release

```bash
make release-check VERSION=vX.Y.Z
make release-dry-run VERSION=vX.Y.Z
make release VERSION=vX.Y.Z
```

Release scripts:
- `scripts/docs-check.sh` validates README command examples against the live CLI command tree and checks docs consistency markers.
- `scripts/release-check.sh` validates version/tag preconditions, runs tests/vet/docs-check/format checks, and verifies stamped version output.
- `scripts/release.sh` runs `release-check`, updates changelog from git history, builds darwin archives, publishes GitHub release/tag, and updates the Homebrew tap formula.
- `scripts/release.sh --dry-run` builds release archives without changelog/tag/push/release/tap writes.
- If the repository does not exist yet, `scripts/release.sh` creates it as **private** by default.
- CI runs `release-check` on `pull_request` and pushes to `main` via `.github/workflows/release-check.yml`.

## Examples

```bash
./acal doctor --json
./acal setup --json
./acal status --json
./acal version
./acal today --json
./acal freebusy --from today --to +7d --json
./acal slots --from tomorrow --to +3d --between 09:00-17:00 --duration 45m --json
./acal today --summary --plain --fields date,total,all_day,timed
./acal week --of today --week-start monday --plain
./acal week --summary --json
./acal month --month 2026-02 --json
./acal view month --month 2026-02 --summary --plain --fields date,total
./acal quick-add "tomorrow 10:00 Standup @Work 30m" --dry-run --json
./acal history list --json
./acal history list --json --limit 10 --offset 10
./acal history undo --dry-run --json
./acal history redo --dry-run --json
./acal queries save next7 --from today --to +7d --where 'title~standup' --limit 10
./acal queries run next7 --json
./acal events quick-add "2026-02-18 09:15 Deep Work @Personal 45m"
./acal events list --from today --to +7d --json
./acal events list --from today --to +7d --verbose --json
./acal events query --from today --to +14d --where 'title~sleep' --sort start --order asc --plain --fields id,title,start,end
./acal events conflicts --from today --to +14d --json
./acal events add --calendar Personal --title "1:1" --start 2026-02-10T10:00 --duration 30m
./acal events add --calendar Work --title "Standup" --start 2026-02-20T09:00 --duration 30m --repeat daily*5
./acal events update <event-id> --location "Room 4A" --scope auto --if-match-seq 1
./acal events update <event-id> --repeat weekly:mon,wed*6 --dry-run --json
./acal events move <event-id> --by 30m --scope auto
./acal events move <event-id> --to 2026-02-20T14:00 --duration 45m --dry-run --json
./acal events copy <event-id> --to 2026-02-21T09:00 --duration 30m --calendar Personal
./acal events remind <event-id> --at -15m --json
./acal events export --from today --to +14d --out calendar.ics
./acal events import --file ./calendar.ics --calendar Work --dry-run --json
./acal events batch --file ./ops.jsonl --dry-run --json
./acal events delete <event-id> --confirm <event-id> --scope auto --no-input
./acal events delete <event-id>   # interactive TTY confirmation prompt
```

## Notes

- Event listing uses the local Calendar SQLite occurrence cache for reliable recurring-instance reads.
- SQLite reads run in-process via `database/sql` (`modernc.org/sqlite`) with read-only immutable mode and per-path connection reuse to reduce lock waits and subprocess/open overhead.
- Writes use AppleScript against Calendar.app.
- Immediately after writes, read cache refresh can lag briefly.
- `status` reports readiness/degraded state plus active backend/profile/tz/output mode for automation diagnostics.
- `status`/`doctor` include machine-friendly `degraded_reason_codes` metadata when checks degrade.
- `--verbose` includes per-command backend timing diagnostics and `meta.timings` in JSON responses.
- Timeout/cancel errors now include backend phase context (for example `backend.list_events timed out...`) to make hang diagnosis faster.
- JSON error payloads include structured timeout/cancel metadata under `meta` (`phase`, `kind`, `deadline`) and map these failures to `BACKEND_UNAVAILABLE` for consistent automation handling.
- Optional transient AppleScript retry controls (off by default):
  - `ACAL_OSASCRIPT_RETRIES` (integer retries; default `0`)
  - `ACAL_OSASCRIPT_RETRY_BACKOFF` (duration; default `200ms`)
- Persistence files (under config dir, usually `~/.config/acal/`):
  - `config.toml`: runtime defaults/profiles.
  - `history.jsonl`: append-only write history for undo.
    - JSONL schema: `{"at","type","tx_id","op_id","event_id","prev","next","created","deleted"}`
  - `redo.jsonl`: redo stack populated by `history undo`.
    - JSONL schema: same as `history.jsonl`.
  - `queries.json`: saved query aliases.
    - JSON schema: `{ "<name>": {"name","from","to","calendars","wheres","sort","order","limit"} }`
- Delete safety model:
  - interactive TTY: prompts for exact event ID unless `--force` or `--confirm` is supplied.
  - non-interactive or `--no-input`: requires `--force` or exact `--confirm <event-id>`.
- Recurring write scope:
  - `--scope auto`: if ID is `<uid>@<occurrence>`, targets one occurrence; otherwise targets full series.
  - `--scope this`: target one occurrence (requires occurrence-style ID).
  - `--scope future`: target this and following occurrences (requires occurrence-style ID).
  - `--scope series`: target the full series.
- Repeat rule grammar (`events add|update --repeat`):
  - `daily*<count>`
  - `weekly:<day[,day...]>*<count>` where day is `mon|tue|wed|thu|fri|sat|sun`
  - `monthly*<count>`
  - `yearly*<count>`
  - Count must be `1..366`.
- History pagination:
  - `history list --limit <n>` returns at most `<n>` most-recent entries (default `10`).
  - `history list --offset <n>` skips `<n>` most-recent entries before applying `--limit`.
