---
status: accepted
---

# Keep Go and qualify one native owner with bounded recovery

Keep acal's CLI in Go. Qualify a bundled, precompiled Swift helper as the sole
owner of Calendar authorization, native resolution, reads, writes and verification,
using a versioned structured protocol. Go retains commands, configuration, output
and workflow orchestration. This is the accepted first candidate, not evidence of
feasibility or authorization to skip the design and first-slice checkpoint.

## Alternatives

Retaining SQLite and AppleScript indefinitely would preserve divergent targeting
and verification paths. In-process integration would change the current CGO-free
build and couple native integration to the Go process. Rewriting the CLI in Swift
would replace useful public behavior without demonstrated benefit. A helper adds
protocol, packaging, process and permission-attribution costs; prove these through
the installed artifact before committing to migration. Users need no compiler.

## Identity and concurrency

Public IDs are opaque local references, not permanent cross-device identifiers.
They may become invalid after synchronization or account changes. Return the
authoritative ID after a verified mutation. Resolve calendar names only when
unique; never select the first duplicate. Reject stale or ambiguous identity and
require rediscovery rather than matching by title and date.

History replay rejects when relevant live state differs from its recorded expected
result, even when an external edit touched a different field. Compare immediately
before writing and verify afterward, without claiming an atomic transaction against
external Calendar edits. Field-level merge is deferred. Exact comparison fields
and ordinary-write revision semantics remain to be specified.

## Recovery and uncertain outcomes

Undo-delete promises recreation of a supported independent event, with a new ID,
not restoration of its original identity. Recovery must include supported alarms
and time semantics. Reject deletion before mutation when properties cannot be
faithfully recreated; do not silently strip invitations or other unsupported data.
The precise supported property set remains an explicit design question.

The first production write slice includes durable operation records: record intent
before mutation and verified results afterward. If pre-write recovery state cannot
be persisted, reject before mutation. A crash or timeout may still leave an unknown
outcome; report it and never automatically retry a potentially applied write. An
operation record does not create a transaction with EventKit or prove remote sync.

Use a separate versioned history store. Preserve legacy files for inspection and
explicit migration; only migrate records whose identity and recovery guarantees
can be established. Carry compatible configuration forward, and return actionable
migration errors for obsolete backend settings or saved IDs. Exact storage schema,
locking, reconciliation and migration tooling remain to be designed.

## Consequences

Some scripts must rediscover events, consume returned IDs or handle migration and
conflict errors. Conservative recovery may reject operations that the old CLI
attempted unsafely. Existing history lacks alarms and other native properties, so
compatibility cannot be achieved by merely changing the backend selector.

Recurring creation, edits, deletion and affected replay remain blocked under
[ADR 0001](0001-restrict-recurring-writes-for-v0.3.0.md), including detached
occurrences. This decision does not establish recurrence scope or history fidelity.

The user accepted Q7–Q12 in the design interview tracked by
[issue #38](https://github.com/agisilaos/acal/issues/38). Deployment scope remains
defined by [ADR 0002](0002-qualify-native-migration-after-v0.3.0.md).
