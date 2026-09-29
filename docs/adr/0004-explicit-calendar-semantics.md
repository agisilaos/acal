---
status: accepted
---

# Make time, recovery and query boundaries explicit

For the proposed native milestone, use explicit time and query semantics rather
than preserving behaviors that silently change user intent. The user delegated
the remaining Q13–Q18 choices after accepting ADRs 0002 and 0003. These decisions
describe the target contract; the current implementation is unchanged.

## Time and alarms

Reject nonexistent local times and ambiguous repeated times unless an explicit
offset resolves the instant. Preserve named-zone and floating-time semantics in
native snapshots. All-day events use date intervals with exclusive end dates, not
fixed 24-hour durations. Reject mutations whose time semantics cannot be preserved.

Initially expose one relative display reminder, before or at the event start.
Replacement replaces all display alarms; clearing removes all display alarms.
Preserve other alarm types during edits. Snapshot every affected supported alarm,
including multiple or absolute display alarms, before replacement or clearing;
reject the operation if faithful restoration is unavailable. Creating after-start
reminders is deferred pending qualification, not silently converted to before-start.

## Read bounds

Retain seven-day list and thirty-day search defaults. Explicit timestamp bounds
use half-open intervals; a date-only end includes that whole local day by advancing
to the next local midnight. Lists select starts within the range; planning selects
overlap. Reject ranges exceeding four calendar years initially, and explicitly
report result truncation. This avoids EventKit's silent four-year shortening
([Apple predicate documentation](https://developer.apple.com/documentation/eventkit/ekeventstore/predicateforevents(withstart:end:calendars:))).

Retain the default 15-second timeout and explicit zero-to-disable option. Bound
helper lifetime on cancellation, but do not equate termination with rollback of a
write. Measure representative cold/warm queries before adding a persistent cache.

## Write and recovery limits

Offer opaque revision tokens for ordinary conditional writes; do not fabricate a
numeric sequence from unsupported native metadata. With a token, reject observed
changes since the read. Without one, operate on the freshly resolved event and
preserve unrelated fields. Compare immediately before mutation and verify after;
neither path is atomic against external Calendar changes.

Initial production writes exclude invitations, attendee-bearing events and events
whose affected native properties cannot be preserved or recovered. They remain
readable. Read-only calendars reject writes before mutation. Qualification expands
the supported property set; unknown fidelity is a reason to reject, not strip data.

## Reserved recurrence meanings

Recurring writes remain blocked, including detached occurrences, under ADR 0001.
For the separately qualified follow-up, reserve these product meanings:

- This: only the selected occurrence.
- Future: the selected occurrence and following occurrences in its currently
  resolved series, preserving earlier occurrences.
- Series: all occurrences and exceptions in the currently resolved series.

Never widen scope automatically or stitch previously split series together.
Exception membership, moved occurrences, native scope mapping and restoration
remain proof obligations in #35. These definitions do not advertise implementation.

## Compatibility and alternatives

Ambiguous DST input will now fail instead of selecting an implicit offset. Explicit
midnight timestamps will no longer expand to the end of that day. After-start
reminder intent must not inherit the old sign-normalization behavior. Numeric
sequence consumers require explicit migration to revision tokens. Existing IDs,
history and saved queries follow ADR 0003's conservative migration policy.

Keeping every historical behavior would preserve ambiguity and incomplete recovery.
Implementing arbitrary recurrence, alarm creation, provider-specific metadata and
unbounded query chunking immediately would expand the first milestone before native
feasibility is established. These boundaries favor reliable, explainable operations.
