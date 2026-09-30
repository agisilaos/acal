# Packaged native proof: bounded consumer review

Reviewed the uncommitted `fix/eventkit-packaged-proof` working tree above `5eeaa34`
for issue #39. The reviewer authored the implementation and had prior code knowledge.
This limits the independence of the public-path usability assessment. Separate
read-only worktree reviewers checked implementation and specification correctness.

## Candidate and environment

- Public route: README → `docs/native-proof.md` → installed `acal --help`,
  `acal native --help`, `acal native events --help`, and the documented workflow.
- Executable version: `acal dev (none) unknown`; exact candidate identity is pinned
  below because development version output alone is insufficient.
- Installation: extracted archive, `bin/acal` plus its adjacent private helper bundle,
  from an unrelated temporary working directory. No development executable was used
  for the live event commands. State and XDG configuration were isolated.
- Host: macOS 27.2 beta, build 26B5091g, arm64, desktop-agent execution,
  noninteractive commands with `--json --no-input`.
- Signing: ad-hoc development package. No Developer ID Application identity is
  available. Stable macOS, notarization, Intel execution and iCloud are unverified.
- Provider tested: one writable local calendar selected from native enumeration.
  No personal events were changed. Calendar names/IDs and unrelated event content
  are omitted from this report.

Package SHA-256 (build 03):

```text
arm64 archive: f71442a58390677ed608f85be0f183375cf4e94cc4b436eb286368c42df8e236
amd64 archive: ae39868afdfaadd9e650b71bb3097e1db96c44e2dc8f5c8840b9b865466cbda0
installed Go executable: 422683eaf66a5bd312acce44adcec6b372533fb0da0188335d266968f494d870
Swift source: 8ae9e17852d9376b7b53fda85b1dcf0f57ddb9caa8fe01341ff46f46252321b0
```

Local detailed evidence is retained in the private temporary directory
`/var/folders/3g/w0d8dz1d26v_577vm4z0j8jr0000gn/T/acal-native-consumer-arey0w6f`:
`public-attempt.json`, `calendar-discovery.json`, `initial-live-attempt.json`,
`workflow.json`, `identity-finding.json`, `fixture-manifest.json`, `cleanup.json`,
`tested-hashes.json` and isolated operation records. Calendar discovery was redacted;
fixture evidence is retained with restrictive directory/file permissions.

## Initial attempt and assisted completion

The initial public help/setup attempt used build 02; setup returned Full Access
without prompting. Independent code review then repaired permission upgrading,
alarm comparison, directory durability and local error isolation before build 03.
The archive was replaced at the same scratch install path; setup again returned
Full Access. This observation does not establish fresh onboarding or permission
persistence for Developer ID upgrades or other launch contexts.

The live workflow used build 03. CALENDAR is the selected local calendar ID, STATE
is the isolated absolute directory, and ID is consumed from each command's result.
The fixture ran seven days after the review at 10:00–10:30 UTC. All commands below
were invoked through installed `acal native` with `--json --no-input`:

| Public command | Exit | Observed result |
| --- | --- | --- |
| `setup` | 0 | `verified`, `ready:true`, `authorization:full_access` |
| `calendars` | 0 | Native calendar IDs, source types and writability returned |
| `events add --state-dir STATE --calendar CALENDAR --title FIXTURE --start START --end END --before 15m` | 0 | Fixture created; title, dates and relative display alarm read back |
| `events show ID` | 0 | Alarm `{type:0, relative_seconds:-900}` observed |
| `events list --calendar CALENDAR --from DAY_START --to NEXT_DAY` | 0 | `truncated:false`; direct ID-string equality with add result failed |
| `events delete ID --state-dir STATE` | 0 | Exact first fixture deleted during cleanup |
| `events show ID` | 6 | `NOT_FOUND`; first fixture absence verified |

The initial automation stopped at the failed ID comparison and cleaned up its exact
fixture. That failure was recorded before inspecting the reference encoding. A
second uniquely marked fixture repeated the workflow, continuing with returned IDs
rather than depending on list-ID equality:

| Public command / relevant recovery | Exit | Observed result |
| --- | --- | --- |
| `events add … --before 15m`, then `events show ID` | 0 | Second fixture and alarm verified |
| `events list …` (three calls) | 0 | ID comparison remained unreliable |
| `events update ID --state-dir DIFFERENT_STATE --title must-not-apply` | 6 | `rejected`, `NOT_OWNED`; no native mutation started |
| `events update ID --state-dir STATE --title 'acal proof edited'` | 0 | Changed title and preserved alarm/dates verified |
| `events remind ID --state-dir STATE --clear` | 0 | Native clear verified |
| `events show ID` | 0 | Empty alarms observed |
| `history undo` / `history redo` under `native` | 2 each | Unknown command; capability explicitly unsupported |
| `events delete ID --state-dir STATE` | 0 | Exact second fixture deleted |
| `events show ID` | 6 | `NOT_FOUND`; second fixture absence verified |

Both fixtures were removed. No fixture calendar was created or deleted. There was
no retry of an uncertain write. Success/error helper responses parsed as JSON on
stdout with empty stderr; local usage errors used the documented schema-v1 JSON
on stderr. The proof never touched production history. Reminder replacement with
another offset, all-day/DST writes, recurrence, invitations, denied/write-only
onboarding and interactive terminal behavior were not live-tested.

## Prioritized findings and proposed separate scope

1. **P2, observed defect, humans and agents: unstable ID strings for the same
   reference.** Five show/add/update results contained three distinct opaque IDs
   with equal decoded calendar/item/start references. Inspection found a default
   JSONEncoder encoding the reference without canonical key order before base64.
   This breaks direct comparisons and reliable joins/deduplication, though the
   different references remain resolvable. Smallest credible fix: canonicalize
   reference encoding, retain backward decoding, add regression coverage, and repeat
   the installed ID comparison workflow. Tracked in
   [#40](https://github.com/agisilaos/acal/issues/40). **Recorded before any fix;
   correction is not part of this review scope.**

2. **Usability judgment, agents: two structured error locations/envelopes.** Native
   helper errors use the proof protocol on stdout; local usage failures use schema
   v1 on stderr. Both are structured and documented, but consumers need two parsing
   paths. Smallest future improvement: unify errors for the experimental route or
   provide one concrete parsing example. This did not prevent assisted completion.

3. **Unsupported capability, not a regression: native undo/redo.** The full requested
   production consumer workflow cannot complete in this first slice. The native
   guide explicitly says production history must not be used for these fixtures.
   Add conflict-safe history only in the separately designed production write slice;
   do not bolt the old snapshot replay onto this proof.

No broad whole-CLI quality claim follows from this small workflow. The experiment
shows usable native CRUD and clearing on this local calendar, with an identity
presentation defect and explicit recovery/qualification limits.

## Other verification and readiness

- Both architecture helpers and Go binaries compile with the declared helper
  deployment target; bundle plist and ad-hoc signatures validate. Only arm64 ran.
- Focused process/protocol/journal tests and `make test vet fmt-check docs-check`
  pass. Two independent read-only reviewers found four issues, which were repaired
  and rechecked; no additional in-scope findings were reported besides #40.
- Three installed one-day query calls took 58.0, 60.5 and 69.2 ms in the assisted
  fixture run. These are smoke timings, not a representative large-calendar
  benchmark or comparison with the legacy backend. No persistent cache was added.
- Fresh authorization, signed distribution/upgrade behavior, stable macOS,
  Intel execution, iCloud and other launch contexts remain unverified.

**Gate: FAIL for shipping.** The recorded ID defect and required deployment
qualification remain unresolved. No commit, push, PR, merge or release is claimed.
A consumer-review fix is a separately proposed scope; publication with missing
qualification additionally requires an explicit risk override under ship-change.

## Authorized follow-up: steps 1–3 (2026-09-27)

The original attempt and findings above remain intact. The user subsequently
approved fixing #40, expanding the proof checks and making package validation
repeatable. Distribution qualification was explicitly deferred.

The ID encoder now uses canonical key order. Decoding still accepts previous valid
`ekp1.` references. A no-save Swift regression checks exact encoded bytes and old
key orders; installed checks compare IDs across separate add/show/list/edit calls.

The expanded workflow first found an at-start reminder readback mismatch in build
04. The write was correctly reported as `applied_unverified`, and the exact fixture
was deleted with absence verified. Alarm readback had compared serialized JSON
bytes, which distinguished signed zero offsets. It now compares native alarm
values; the regression treats positive/negative zero equally while distinguishing
absolute alarms and nonzero offsets.

Build 05 passed the reusable workflow:

```bash
make native-proof-test
scripts/package-native-proof.sh --ad-hoc /tmp/acal-native-proof-build-05
python3 scripts/native-proof-smoke.py --archive /tmp/acal-native-proof-build-05/acal-native-proof-darwin-arm64.tar.gz --local --allow-calendar-writes
```

Identity and evidence:

```text
version: acal native-proof.66460def7994 (5eeaa34a0d07ccabaacc6152f9638f652577109e-dirty) 2026-09-27T08:15:14Z
arm64 archive SHA-256: fce1f9d035c8d6d02616dd66985077b7a1fc36096071fa6c1bab9211ec67f9af
source SHA-256: 66460def79942b2b6876ac1b520354d9da6a06ab769fa2b335c4e23c14730436
passed evidence: /var/folders/3g/w0d8dz1d26v_577vm4z0j8jr0000gn/T/acal-native-smoke-igheqaj_
initial at-start failure: /var/folders/3g/w0d8dz1d26v_577vm4z0j8jr0000gn/T/acal-native-smoke-m83mbmys
```

The final commands returned exit 0 for setup, enumeration, creation, repeated
show/list equality, old-ID lookup, title edit, 30-minute replacement, at-start
replacement, clearing and deletion. The wrong-state update returned exit 6 with
`NOT_OWNED`; an immediate show confirmed unchanged fixture fields. The final show
returned exit 6 with `NOT_FOUND`. Both follow-up fixtures were deleted and absence
verified. The installed binary's reported version matched its `BUILD-INFO.json`.

`make test vet fmt-check docs-check native-proof-test` passed. Both architecture
archives build; only arm64 ran. The native tests use an actual unsaved recurring
EKEvent and supplied detached/read-only policy states; they do not qualify saved
detached occurrences or provider read-only behavior. Two independent read-only
reviewers found no new blocking implementation issues in the follow-up.

**Steps 1–3 pass within this proof's scope.** #40 is fixed and verified locally;
its issue stays open until the uncommitted branch work is published. The earlier
shipping failure is no longer blocked by #40, but distribution/stable qualification
remains incomplete. No production undo/redo, provider-wide support, commit, push,
PR, merge or release is claimed. The original error-envelope usability judgment
remains a possible later improvement.

## Homebrew follow-up (2026-09-29)

The user selected Homebrew without notarization and authorized continuation. ADR
0005 replaces the mandatory notarization policy; stable/provider/launcher validation
remains required. This follow-up preserves the earlier attempts above.

A generated **prebuilt-archive formula** (not a cask or Homebrew bottle) exposes
`acal-native-proof` and preserves the binary/helper relative layout. The actual
production tap uses a similar prebuilt-archive model and was not edited. The new
checker refuses an existing proof installation and uses a unique local tap.

The initial install attempt failed before installation because Homebrew 7 rejects
combining `depends_on :macos` and an unconditional `depends_on macos:`. The generated
formula was corrected to place the minimum version inside `on_macos`. The failed
attempt and cleanup remain recorded at:

```text
/var/folders/3g/w0d8dz1d26v_577vm4z0j8jr0000gn/T/acal-homebrew-evidence-ld58xdqj
```

The corrected public installation path passed:

```bash
python3 scripts/test-native-homebrew.py --old-package-dir /tmp/acal-homebrew-proof-01 --new-package-dir /tmp/acal-homebrew-proof-02 --local --allow-calendar-writes
```

The checker generated a unique tap, ran `brew install --formula`, `brew test`, then
`brew upgrade --formula` and `brew test`, verified installed signatures, and used
`/opt/homebrew/bin/acal-native-proof` for noninteractive setup and each fixture
workflow. It did not run the archive-extracted executable for these Calendar checks.
The installed Go executable resolves the Homebrew symlink before locating its helper.

| Stage | Installed identity | Result |
| --- | --- | --- |
| Initial install | `native-proof.bf234df9afb1`, 2026-09-29T11:32:15Z | brew install/test, signatures, setup and owned-fixture workflow passed |
| Upgrade | `native-proof.2321836b8498`, 2026-09-29T11:33:02Z | brew upgrade/test, signatures, setup and owned-fixture workflow passed |

The helper executable hashes differed:

```text
before: 982d9e763c35b1631ae3120fce95d6cac0500f5d33f1281aa602c3cbc70722c9
after:  e25c66410998fd2cf1dd98d190742a62ca2f47cda5d2bbd278de93922231b63d
```

Both setup commands returned Full Access without prompting in the desktop-agent
launch context. The new helper's reported build version/date matched its installed
manifest. This proves an observed existing-grant upgrade across different helper
bytes and keg paths here, not a universal permission-continuity guarantee.

Both Homebrew-installed workflow runs passed canonical/previous-order ID lookup,
show/list equality, title update, reminder creation/replacement/at-start/clear,
wrong-state rejection and exact deletion/absence. Both fixture manifests confirm
cleanup. The successful run removed its proof formula and tap, and the checker
confirmed production acal was unchanged. The earlier failed run had a broader
cleanup side effect, documented below.

Evidence:

```text
Homebrew lifecycle: /var/folders/3g/w0d8dz1d26v_577vm4z0j8jr0000gn/T/acal-homebrew-evidence-sm3hfyyk
first installed fixture run: /var/folders/3g/w0d8dz1d26v_577vm4z0j8jr0000gn/T/acal-native-smoke-41flha_r
upgraded fixture run: /var/folders/3g/w0d8dz1d26v_577vm4z0j8jr0000gn/T/acal-native-smoke-wq8q0vam
```

This remains beta macOS 27.2, arm64, local-calendar evidence. Local file-URL archives
and an existing grant do not qualify public HTTPS downloads, fresh consent, Terminal
execution, stable macOS, Intel execution or iCloud. No notarization credentials,
quarantine removal or Gatekeeper changes were used. The original author still has
implementation knowledge; this is a bounded installation/workflow review, not an
independent novice usability test. Production undo/redo remains unsupported here.


### Cleanup defect and verified repair

Independent correctness review found that Homebrew uninstall runs autoremove even
with `HOMEBREW_NO_INSTALL_CLEANUP=1`. The initial failed installation's cleanup
removed unrelated orphan dependencies `libnghttp3 1.18.0` and `libngtcp2 1.25.0`.
The original production-acal-only snapshot did not detect this. Both dependencies
were restored at those exact versions with `brew install --as-dependency`, with
auto-update, install cleanup, autoremove and dependent checks disabled. This
restores the versions and dependency status, not a claim of byte-identical receipts.

The checker now disables autoremove for every Homebrew subprocess, skips uninstall
when there is no owned keg, and compares the complete installed formula/version
inventory before and after the lifecycle. Isolation failures mark the result failed.
A read-only independent re-review found no further actionable defects in the fix.

The same install/upgrade and sequential owned-fixture workflow passed again after
this repair. Cleanup removed the proof formula and tap, preserved production acal,
and preserved every installed formula version, including the restored dependencies.
Evidence (including both inventories and command logs):

```text
/var/folders/3g/w0d8dz1d26v_577vm4z0j8jr0000gn/T/acal-homebrew-evidence-o1ztxp4x
```

### Final local readiness

Go tests, vet, formatting, documentation checks and native Swift proof tests passed
for this working tree. The Homebrew lifecycle passed within the scope above.
The release/migration qualification gate remains **FAIL**: stable macOS, Intel,
iCloud, fresh consent, Terminal and public HTTPS delivery remain unverified.
Notarization is no longer a required gate under ADR 0005. No commit, push, production
tap update or release was performed.

## Repository status — 2026-09-30

The candidates, failures and publication status above describe the historical
runs. The native proof, canonical-ID and signed-zero fixes, and Homebrew lifecycle
safeguards landed in [PR #42](https://github.com/agisilaos/acal/pull/42), merged
2026-09-29 at `02a1938`. Production creation-recovery and installation/documentation
follow-ups landed in [PR #43](https://github.com/agisilaos/acal/pull/43), merged
2026-09-30 at `0f85538`.

The proof is committed on `main`; its installed deployment qualification remains
incomplete. The [current qualification matrix](../native-proof.md#remaining-qualification)
separates recorded beta/local evidence from stable macOS, Intel, iCloud, fresh
consent, Terminal and public HTTPS obligations. This status correction adds no
live qualification evidence and does not establish production migration or a
published release.
