# Experimental installed EventKit proof

This candidate retains the Go CLI and adds `acal native`, which calls a bundled
precompiled Swift helper. It does not enable the production `--backend eventkit`
selector. Existing commands still use the existing backend and history.

The proof is not production qualified. It supports timed independent fixtures,
calendar/occurrence reads and display-alarm changes. It does not support all-day
writes, recurrence, invitations, production history, undo/redo or legacy ID migration.
The accepted broader contracts in the ADRs are subsequent migration work.

## Build and install a development candidate

Maintainers need Go, Python 3, Xcode/Swift and macOS to build. End users need no compiler.
The output directory must be absolute and not already exist:

```bash
scripts/package-native-proof.sh --ad-hoc /tmp/acal-proof-build
mkdir -p /tmp/acal-proof-install
# Use amd64 instead of arm64 on Intel.
tar -xzf /tmp/acal-proof-build/acal-native-proof-darwin-arm64.tar.gz -C /tmp/acal-proof-install
/tmp/acal-proof-install/bin/acal native --help
```

Keep `bin` and `libexec` together. A symlink to the installed binary is supported.
This script deliberately produces **ad-hoc signed development archives**, not a
Developer ID or notarized distribution. The selected Homebrew route does not require those credentials; supported stable
macOS qualification remains required before production cutover. The current
beta host cannot establish them. Production release packaging is unchanged.

## Discover and complete the fixture workflow

`native` always emits JSON; helper responses use `acal-native-proof-v1`. It ignores config
files and `ACAL_*` environment variables; it rejects incompatible global flags.
Use a fresh absolute `--state-dir`. Keep this directory until all created fixtures
are removed: its ownership token is required for mutation and cleanup.

```bash
/tmp/acal-proof-install/bin/acal native setup --json
# Only this explicit operation may request permission; run it interactively.
/tmp/acal-proof-install/bin/acal native setup --request-access --timeout 2m --json
/tmp/acal-proof-install/bin/acal native calendars --json
```

Choose an exact writable calendar ID from `data.calendars`. Do not infer provider
support from one calendar. The helper reports the native source type; local is 0,
CalDAV is 2 (which alone does not identify iCloud).

Substitute the selected calendar ID below. Use dates appropriate for the run:

```bash
/tmp/acal-proof-install/bin/acal native events list --calendar CALENDAR_ID --from 2026-10-01T00:00:00Z --to 2026-10-02T00:00:00Z --json
/tmp/acal-proof-install/bin/acal native events add --state-dir /tmp/acal-proof-state --calendar CALENDAR_ID --title 'acal disposable proof' --start 2026-10-01T10:00:00Z --end 2026-10-01T10:30:00Z --before 15m --json
```

Use the returned `data.id` as EVENT_ID; consume the returned ID after each edit.
IDs are opaque local references, not permanent identifiers. The fixture carries
an ownership URL and notes marker; leave them intact until cleanup.

```bash
/tmp/acal-proof-install/bin/acal native events show EVENT_ID --json
/tmp/acal-proof-install/bin/acal native events update EVENT_ID --state-dir /tmp/acal-proof-state --title 'acal disposable proof edited' --json
/tmp/acal-proof-install/bin/acal native events remind EVENT_ID --state-dir /tmp/acal-proof-state --clear --json
/tmp/acal-proof-install/bin/acal native events show EVENT_ID --json
/tmp/acal-proof-install/bin/acal native events delete EVENT_ID --state-dir /tmp/acal-proof-state --json
/tmp/acal-proof-install/bin/acal native events show EVENT_ID --json
```

The final show should report NOT_FOUND. Reminder replacement uses `--before 30m`,
with a nonnegative number of whole minutes; `--before 0` means at start. It replaces
all display alarms, preserving other types. There is no native-proof undo/redo:
production `history` cannot recover these fixtures and must not be used for them.

## Outcomes and recovery limits

Each native response contains `protocol`, `request_id`, `outcome`, and either `data`
or `error`. Success exits 0. Helper/permission/native errors exit 6. Local usage
errors exit 2 using the existing CLI error envelope on stderr, always as JSON. This experimental protocol is distinct from production schema v1.

- `verified`: local EventKit readback matched the operation. This is not proof of
  server or other-device synchronization.
- `rejected`: no native mutation was started.
- `applied_unverified`: a save/delete returned, but readback or durable recording
  did not establish the complete result.
- `unknown`: a started helper may have mutated Calendar before interruption/failure.

Never automatically retry an uncertain write, especially creation. The state
folder contains private intent/result JSON records named by request ID. A record
without a result also has unknown completion. Keep this evidence and inspect the
fixture through bounded reads before deciding what to do; automatic reconciliation
and production recovery are outside this proof. Cleanup only exact fixtures owned
by this state, and run mutations sequentially. Do not remove a calendar or match
personal events by title. An add failure may leave a fixture requiring inspection.

Native date arguments require valid calendar dates with an explicit RFC3339
offset and whole seconds. UTC accepts `Z` or `z`. Invalid dates, malformed offsets,
fractional seconds, and trailing characters return `INVALID_USAGE`.

Query intervals are explicit RFC3339 instants, start-inclusive/end-exclusive, at
most four years. Listing uses event start times, not overlap. `truncated` reports
the result limit. The default timeout is 15 seconds; zero disables it. Termination
bounds waiting but cannot roll back a write.

## Repeatable validation and build identity

`acal version` now identifies the native proof by source hash, Git revision, dirty
state and UTC build time. Each archive includes `BUILD-INFO.json` with full source
hashes and compiler versions. The two architecture packages share a source identity;
archive checksums identify their separate outputs. Build metadata distinguishes a
clean commit from a candidate containing uncommitted changes.

Run the no-save checks before packaging:

```bash
make native-proof-test
```

These compile the real Swift entry point, exercise canonical and prior-order ID
decoding, alarm-value comparison, a native in-memory recurring event, and detached/
read-only rejection policy. They never save Calendar data or request access. The
macOS CI workflow also runs this target. Synthetic detached/read-only policy tests
are not evidence of real provider behavior.

Run the installed-package workflow only when Full Calendar Access already exists:

```bash
python3 scripts/native-proof-smoke.py --archive /tmp/acal-proof-build/acal-native-proof-darwin-arm64.tar.gz --local --allow-calendar-writes
```

`--local` requires exactly one writable local calendar. Alternatively, supply
`--calendar CALENDAR_ID` to select a writable calendar explicitly; do not infer
provider qualification from that choice. The script installs into a private scratch
location and retains build, command, fixture and cleanup evidence there. It tests
ID equality across add/show/list/edit, prior-order ID decoding, ownership rejection,
reminder replacement, at-start reminders, clearing and verified deletion. It creates
one unique fixture, uses isolated state and mutates sequentially. It never retries
an uncertain creation or deletion. An interrupted run can require inspection of
its retained fixture manifest and operation records.

## Homebrew without notarization

The approved distribution path is now an ad-hoc-signed **binary formula**, as
recorded in [ADR 0005](adr/0005-homebrew-without-notarization.md). It installs
precompiled archives; it is not a cask or a Homebrew-built bottle. It needs no user
compiler or Developer ID/notarization credentials. The private helper `.app` contains
metadata, not a user-facing GUI. Existing `acal` commands/history stay unchanged.

Render a local formula after packaging:

```bash
python3 scripts/native-proof-formula.py --package-dir /tmp/acal-proof-build > /tmp/acal-native-proof.rb
```

For a future published tap, supply `--base-url https://HOST/immutable/package/path`.
The renderer inserts both architecture checksums, a timestamp/source version, a
macOS 14 floor, package layout and a no-Calendar-access `brew test`. It does not
upload archives, edit the real tap or publish a release. Local qualification uses
file URLs; public HTTPS installation remains a separate qualification requirement.

For safe local install/upgrade qualification, build two successive candidates into
new directories, then run:

```bash
python3 scripts/test-native-homebrew.py --old-package-dir /tmp/acal-proof-build-old --new-package-dir /tmp/acal-proof-build-new --local --allow-calendar-writes
```

The checker refuses an existing `acal-native-proof` installation, creates a unique
local tap, installs and upgrades the fully qualified formula, runs `brew test`,
verifies installed signatures and exercises the actual Homebrew public command
through the fixture workflow. It requires different installed helper bytes, retains
logs, and removes its own formula/tap with Homebrew autoremove disabled. It checks
that production `acal` and the complete installed formula/version inventory were
not changed. It never resets permission grants or weakens Gatekeeper.

The generated formula exposes `acal-native-proof`; it does not expose or replace
`acal`. An existing Homebrew installation can also be exercised directly:

```bash
python3 scripts/native-proof-smoke.py --binary /opt/homebrew/bin/acal-native-proof --local --allow-calendar-writes
```

Use the installed path under your Homebrew prefix (typically `/usr/local` on Intel).
The smoke test executes that public symlink, resolves the real executable only to
locate its build metadata, and uses a fresh isolated state directory.

## Remaining qualification

Calendar authorization and distribution trust are distinct. The user replaced the
earlier notarization policy; it is no longer a gate for this Homebrew target. The
CLI still needs Full Calendar Access, truthful recovery guarantees and testing on
supported stable macOS. No Apple Developer enrollment is needed for this path.

Use a stable macOS host or VM with a logged-in desktop session, disposable local/
iCloud fixtures and the intended terminal/agent launch contexts. Intel execution
still needs coverage if advertised. Test first consent and successive installations;
existing-grant success in one launcher does not prove cross-launcher persistence.
A local tap install on a beta host does not qualify all downloaded software or
public delivery. Preserve these limits in the evidence rather than requiring users
to remove quarantine attributes or disable system protections.

The proof and its local Homebrew lifecycle landed in [PR #42](https://github.com/agisilaos/acal/pull/42).
The recorded results remain limited to the combinations tested in the
[consumer review](reviews/native-proof-consumer-review.md); merge and routine CI
do not establish additional deployment coverage.

| Qualification | Recorded evidence | Remaining acceptance |
| --- | --- | --- |
| Installed CRUD, alarms and IDs | Passed on macOS 27.2 beta, arm64, one local calendar, desktop-agent execution | Repeat installed workflows on the advertised stable combinations |
| Homebrew install and upgrade | Passed with local file URLs and different helper bytes/kegs in the same environment | Qualify public HTTPS delivery and stable-system installation/upgrade |
| Calendar consent | Existing Full Access observed across the tested upgrade | Test first consent, denied/restricted access and each intended launcher |
| Terminal execution | Unverified for the installed native proof | Run the installed public commands from Terminal |
| iCloud | Unverified | Use an exact disposable iCloud fixture and record provider identity; source type alone is insufficient |
| Intel execution | Both architecture archives compile; no installed Intel execution evidence | Run the installed workflow on Intel if advertised |
| Production migration | Experimental fixtures only; no production undo/redo or legacy migration | Complete the bounded reads, revisions, recovery/history and migration slices |

Retain package hashes/build metadata, OS/build, architecture, provider, launcher,
consent state, public commands, outcomes, native readback and exact cleanup for
each run. Homebrew lifecycle evidence must also show unchanged production acal and
installed formula inventory. Track remaining deployment work in
[#39](https://github.com/agisilaos/acal/issues/39) and
[#41](https://github.com/agisilaos/acal/issues/41). The separate v0.3.0 production
release still needs its own supported stable-macOS archive qualification under
[#34](https://github.com/agisilaos/acal/issues/34).
