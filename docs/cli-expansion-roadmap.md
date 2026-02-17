# CLI Expansion Roadmap

This roadmap sequences 10 CLI improvements with a test-and-doc-first workflow.

## Workflow contract per step

1. Update docs/spec for the step.
2. Implement the command/behavior.
3. Add or update tests.
4. Run `go test ./...` and stop.
5. Proceed to the next step only after review.

## Steps

1. Conflict detection (`events conflicts`) with deterministic output.
2. Free/busy summary (`freebusy`) for planning windows.
3. Slot finder (`slots`) for duration-aware scheduling.
4. Reminder/alert management on events.
5. Recurrence creation/editing UX (`--repeat` grammar).
6. ICS export for ranges/calendars.
7. ICS import with dry-run and validation diagnostics.
8. Batch operations for add/update/delete via JSONL.
9. History/undo for write operations.
10. Saved queries/aliases for repeated agent workflows.

## Exit criteria

- Every step has docs, tests, and passing suite.
- At step 10, run an agentic-friendliness review and summarize changes.

## Completion status

- [x] Step 1: Conflict detection (`events conflicts`)
- [x] Step 2: Free/busy summary (`freebusy`)
- [x] Step 3: Slot finder (`slots`)
- [x] Step 4: Reminder metadata command (`events remind`)
- [x] Step 5: Recurrence UX (`--repeat`)
- [x] Step 6: ICS export (`events export`)
- [x] Step 7: ICS import (`events import`)
- [x] Step 8: Batch operations (`events batch`)
- [x] Step 9: History and undo (`history list|undo`)
- [x] Step 10: Saved queries and aliases (`queries save|list|run|delete`)
