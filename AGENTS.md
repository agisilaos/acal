## Sessions and landing

- Start each new implementation chat in its own worktree with `scripts/session start <name> [--issue <number>]`. Read [the session workflow](docs/agents/sessions.md) when preparing a kickoff, working alongside another chat, or landing and closing a session.
- Review and investigation requests end with findings before edits. Commit and land with the user's authorization for each step.
- From the main checkout, land committed work with `scripts/session land --worktree <path>`. It runs Codex review and `make verify`, then fast-forwards `origin/main` directly. Stage only paths owned by this session.

## Agent skills

### Issue tracker

Track issues and specs in GitHub Issues for agisilaos/acal. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the five canonical triage labels. See `docs/agents/triage-labels.md`.

### Domain docs

Use a single root context and `docs/adr/`. Before domain or architecture work, read `docs/agents/domain.md`.
