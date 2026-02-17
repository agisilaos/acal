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
- `queries save`
- `queries list`
- `queries run`
- `queries delete`

## Output modes

- `--json` envelope output for agents
- `--jsonl` streaming object-per-line output
- `--plain` stable line-based output
- `--verbose` diagnostics to stderr (resolved command/backend/mode/profile)
- `--no-color` disable ANSI coloring in human-readable errors (also auto-disabled by `NO_COLOR` or `TERM=dumb`)

## Config and precedence

Supported precedence: `flags > env > project config > user config > defaults`

- User config: `~/.config/acal/config.toml` (or `$XDG_CONFIG_HOME/acal/config.toml`)
- Project config: `./.acal.toml`
- Env vars:
  - `ACAL_PROFILE`
  - `ACAL_BACKEND`
  - `ACAL_TIMEZONE`
  - `ACAL_OUTPUT` (`json|jsonl|plain`)
  - `ACAL_FIELDS`
  - `ACAL_NO_INPUT`

## Build

```bash
go build ./cmd/acal
```

## Release

```bash
make release-check VERSION=v0.1.0
make release VERSION=v0.1.0
```

Release scripts:
- `scripts/release-check.sh` validates changelog format, runs tests/vet, and verifies stamped version output.
- `scripts/release.sh` updates changelog, builds darwin archives, publishes GitHub release/tag, and updates Homebrew tap formula.
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
./acal history undo --dry-run --json
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
- Writes use AppleScript against Calendar.app.
- Immediately after writes, read cache refresh can lag briefly.
- `status` reports readiness/degraded state plus active backend/profile/tz/output mode for automation diagnostics.
- Delete safety model:
  - interactive TTY: prompts for exact event ID unless `--force` or `--confirm` is supplied.
  - non-interactive or `--no-input`: requires `--force` or exact `--confirm <event-id>`.
- Recurring write scope:
  - `--scope auto`: if ID is `<uid>@<occurrence>`, targets one occurrence; otherwise targets full series.
  - `--scope this`: target one occurrence (requires occurrence-style ID).
  - `--scope future`: target this and following occurrences (requires occurrence-style ID).
  - `--scope series`: target the full series.
