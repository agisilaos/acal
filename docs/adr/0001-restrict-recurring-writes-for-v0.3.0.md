---
status: accepted
---

# Restrict recurring-event writes for v0.3.0

For v0.3.0, reject recurring-event writes before mutation and defer complete recurrence support to a follow-up. Native probes show that occurrence edits can detach an instance or split a series, changing identities in ways the current readback and history model cannot consistently represent. A lookup-only fix would therefore leave verification and replay unreliable. See [issue #34](https://github.com/agisilaos/acal/issues/34).

We chose a bounded release restriction over implementing coordinated native identity, readback, and history changes before this release. Recurring-event reads remain available with their existing limitations.

[Issue #35](https://github.com/agisilaos/acal/issues/35) owns the full-support follow-up; [issue #36](https://github.com/agisilaos/acal/issues/36) tracks the broader CLI experience work.

The user also accepted rejection of recurring creation and adding recurrence, writes to detached occurrences, and replay that would perform a blocked write. Rejection leaves both history stacks unchanged. Existing-event writes require Full Calendar Access for EventKit classification; absent access or uncertain classification stops before mutation with actionable guidance.

Legacy recreation snapshots cannot prove independence and must remain blocked; new successful guarded writes retain that evidence for replay. Public failures distinguish known pre-mutation rejection from an uncertain write. Full recurrence support and provider qualification remain follow-up work.
