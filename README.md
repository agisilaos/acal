# acal

`acal` is a Go CLI for querying and managing Apple Calendar with human and agent-friendly output.

## Implemented

- `doctor`
- `setup`
- `calendars list`
- `events list`
- `events search`
- `events query` (`--where`, `--sort`, `--order`, `--limit`)
- `events show`
- `events add`
- `events update`
- `events delete`
- `agenda`
- `today`
- `week`
- `month`
- `view`
- `quick-add`
- `completion`

## Output modes

- `--json` envelope output for agents
- `--jsonl` streaming object-per-line output
- `--plain` stable line-based output

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

## Examples

```bash
./acal doctor --json
./acal setup --json
./acal today --json
./acal today --summary --plain --fields date,total,all_day,timed
./acal week --of today --week-start monday --plain
./acal week --summary --json
./acal month --month 2026-02 --json
./acal view month --month 2026-02 --summary --plain --fields date,total
./acal quick-add "tomorrow 10:00 Standup @Work 30m" --dry-run --json
./acal events quick-add "2026-02-18 09:15 Deep Work @Personal 45m"
./acal events list --from today --to +7d --json
./acal events query --from today --to +14d --where 'title~sleep' --sort start --order asc --plain --fields id,title,start,end
./acal events add --calendar Personal --title "1:1" --start 2026-02-10T10:00 --duration 30m
./acal events update <event-id> --location "Room 4A" --if-match-seq 1
./acal events delete <event-id> --confirm <event-id> --no-input
```

## Notes

- Event listing uses the local Calendar SQLite occurrence cache for reliable recurring-instance reads.
- Writes use AppleScript against Calendar.app.
- Immediately after writes, read cache refresh can lag briefly.
