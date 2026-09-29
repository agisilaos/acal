---
status: accepted
---

# Qualify the native migration after v0.3.0

The Developer ID/notarization policy below was subsequently replaced by
[ADR 0005](0005-homebrew-without-notarization.md). Other scope decisions remain active.

The proposed EventKit-only migration targets the next milestone independently of
v0.3.0. Preserve the recurring-write restriction in [ADR 0001](0001-restrict-recurring-writes-for-v0.3.0.md)
while qualifying independent-event workflows. The CLI remains in Go, with a
bundled compiled Swift helper selected as the first candidate to qualify in
[ADR 0003](0003-native-ownership-and-safe-recovery.md). EventKit-only feasibility
still needs proof. Design and first-slice agreement must precede implementation.

The initial target matrix is macOS 14 and later, Apple Silicon and Intel, with
local and iCloud calendars. Initial execution support covers terminals and desktop
agents in a logged-in desktop session. Setup may request permission explicitly;
ordinary noninteractive commands must return actionable permission errors. Other
providers and unattended launchd/SSH execution require separate qualification.
These are qualification targets, not claims of coverage already achieved.

We accept maintaining Developer ID signing and notarization and packaging a native
component while retaining Homebrew and archive installation. Qualify the installed
artifact and an upgrade before promising permission persistence. One approval
across all launch contexts is not assumed. No runtime compiler is acceptable for
users; exact package shape remains open.

## Alternatives and migration implications

Including the migration in v0.3.0 would expand a bounded safety release before
native feasibility is established. Supporting older macOS versions immediately
would add authorization paths; claiming more providers or unattended contexts
without tests would overstate coverage. macOS 14 provides the explicit full-access
API needed for reads and writes ([Apple TN3153](https://developer.apple.com/documentation/technotes/tn3153-adopting-api-changes-for-eventkit-in-ios-macos-and-watchos)).

Preserve command syntax and structured output where truthful, but allow an explicit
migration boundary with versioned IDs and history when safe targeting cannot
preserve existing contracts. Reject legacy entries that cannot be resolved safely;
do not guess targets or silently discard history. Identity and recovery policy
are further defined in ADR 0003; exact schemas and migration tooling remain open.

This avoids making compatibility an excuse for retaining unreliable targeting,
while acknowledging that existing scripts and recovery records may need migration.
Stable macOS and provider qualification remain required; current beta-host evidence
does not satisfy that requirement. Signing credentials and stable test environments
have not yet been verified.

The user accepted this scope in the first design round. [Issue #38](https://github.com/agisilaos/acal/issues/38)
tracks unresolved design work under [#36](https://github.com/agisilaos/acal/issues/36);
[#35](https://github.com/agisilaos/acal/issues/35) retains full recurrence support.
Merge and release still require explicit authorization.

## Distribution clarification

Developer ID signing and notarization are the chosen eventual distribution policy,
not prerequisites imposed by EventKit for all local CLI execution. The helper's
private app bundle is a metadata/package choice; acal remains a Go CLI and does
not require an App Store listing. The user deferred arranging distribution/stable
qualification while authorizing the local ID fix, reminder tests and repeatable
package proof. This clarifies the decision without changing the release target.
