---
status: accepted
---

# Qualify Homebrew distribution without notarization

Keep the Go CLI and compiled Swift EventKit helper, but replace ADR 0002's mandatory
Developer ID/notarization distribution policy with an ad-hoc-signed Homebrew route.
The user explicitly wants to avoid managing notarization and approved continuing
this path. This supersedes only the distribution portion of ADR 0002; stable macOS,
provider, architecture and permission qualification remain required.

Use a prebuilt-archive formula in the existing tap model. This is a binary formula,
not a cask or a Homebrew-built bottle. The current production tap already installs
prebuilt acal archives. Start with the separate `acal-native-proof` command so
qualification does not replace production acal or expose legacy history through
the experimental backend. Keep native helper metadata and package-relative lookup.
No user compiler, Apple Developer account or notarization credential is required
by this installation path. Do not modify quarantine attributes or disable Gatekeeper.

## Alternatives

A source-build formula remains a fallback if the prebuilt route cannot meet the
required installation/permission behavior, but would introduce installation-time
build dependencies. True Homebrew bottles can be considered later; they are not
necessary to prove the current binary formula. Replacing Swift with AppleScriptObjC
or moving EventKit into Go would spend implementation effort without establishing
better installation or permission behavior. A future notarized archive may be
optional; it is no longer a prerequisite for this proof or its Homebrew target.

## Qualification and migration

Test fresh installation and a different package build through Homebrew, preserving
the existing production installation. Verify the helper path after symlink/keg
changes, signatures after installation, setup, owned fixture operations and cleanup.
A revision-only update of identical bytes does not establish helper-update behavior.
Do not infer fresh consent from an existing grant or infer Terminal behavior from a
desktop agent. A local file-URL tap test does not establish public HTTPS delivery.

Stable OS and advertised provider/architecture coverage still matter. The product
remains a CLI; the private helper app bundle is an implementation detail, not an App
Store submission. Permission persistence remains evidence-dependent, and uncertain
writes, recurrence restrictions and conservative history migration remain unchanged.
