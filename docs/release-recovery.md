# Recovering an interrupted release

`scripts/release.sh` publishes a version tag, then a GitHub release with assets, then the Homebrew formula. If a step fails, it stops and reports which commands succeeded, which were not attempted, and which have an unknown outcome. These are observations from that invocation, not a fresh inspection of the servers. A command can succeed remotely and still return an error locally.

There is no automatic resume. Keep the failure output and the original `dist/` archives, `SHA256SUMS`, `NOTES.md` and rendered formula. Do not rerun release or release-dry-run to recover: both rebuild archives, and their bytes and checksums can change. A prepared Homebrew work directory is also retained after failure for inspection; do not blindly push it. Remove it manually once recovery is complete. Module preflight uses disposable metadata and leaves repository module files unchanged.

## Inspect before making changes

Run checks from the same checkout and GitHub CLI environment as the failed attempt. Use the intended version below:

```bash
VERSION=vX.Y.Z
GITHUB_REPO=owner/repository
git rev-parse HEAD
git rev-parse "refs/tags/$VERSION^{commit}"
git ls-remote origin "refs/tags/$VERSION" "refs/tags/$VERSION^{}"
gh release view "$VERSION" --repo "$GITHUB_REPO" --json url,assets
```

Compare the local and remote tag commits with the full expected release commit printed by the failure report. For an annotated remote tag, compare the peeled `^{}` commit. A missing local tag makes `rev-parse` fail; an absent remote tag produces no matching refs only when `ls-remote` itself succeeds. Resolve authentication, repository-selection, and network errors before concluding anything is absent. Confirm that GitHub asset URLs and the intended formula refer to the same release repository; `GITHUB_REPO` controls both formula URLs and the explicit GitHub CLI publishing repository. Tag pushes still use `origin`; verify that both identify the intended repository.

Never force-move or delete a tag to make the release script pass. If commits disagree, stop and investigate.

## Finish only the missing step

| Interrupted step | Manual next action after inspection |
| --- | --- |
| Preflight or artifact preparation | This run attempted no publication. Fix the reported problem; before rebuilding, ensure an earlier attempt has not already published this version. |
| Local tag creation or tag push | Verify any existing tag against the expected commit. If the local tag is missing, create it at that exact commit. If the remote tag is absent, push the matching local tag without force; if present and matching, leave it alone. |
| GitHub release/assets | Inspect the release and each asset. If the release is confirmed absent, create it using the existing remote tag and original retained files. If partially populated, verify existing assets against the originals and upload only missing files, without overwriting anything. |
| Homebrew commit or push | Verify the complete GitHub release first. Read the latest formula on the configured tap branch. If its version, URLs, and hashes already match, no work remains. If it is newer, leave it alone. Otherwise update only the missing Homebrew work using hashes of the downloaded published archives. |

## Verify bytes before uploading or updating Homebrew

Check that the retained originals still match the checksum file from the failed attempt:

```bash
(cd dist && shasum -a 256 -c SHA256SUMS)
```

This checks consistency, not provenance. Use these files only if you know they are the untouched originals from the recorded commit. If they were rebuilt, overwritten, or their origin is uncertain, stop rather than guessing.

Download existing GitHub assets into a separate empty directory, leaving `dist/` untouched:

```bash
download_dir="$(mktemp -d)"
gh release download "$VERSION" --repo "$GITHUB_REPO" --dir "$download_dir"
```

Compare downloaded files byte-for-byte with the corresponding retained originals using `cmp`. Any mismatch, including a differing published `SHA256SUMS`, requires investigation; do not overwrite it. For a partial release, upload only absent original files, then repeat the download commands above with a new empty directory to verify the complete set. The expected files are both `acal_<version-without-v>_darwin_amd64.tar.gz` and `acal_<version-without-v>_darwin_arm64.tar.gz`, plus `SHA256SUMS`.

Once all three are present and verified, check the downloaded archives against their downloaded checksums:

```bash
(cd "$download_dir" && shasum -a 256 -c SHA256SUMS)
```

Use the hashes verified from those published archives for Homebrew. Inspect the current version, architecture URLs, and hashes in `HOMEBREW_FORMULA_PATH` (default `Formula/acal.rb`) on `HOMEBREW_TAP_BRANCH` (default `main`) in `HOMEBREW_TAP_REPO` (default `agisilaos/homebrew-tap`). Preserve unrelated formula content. If the same version has different URLs or hashes, investigate instead of replacing it; never downgrade a newer version. Push any required update without force. If a write reports an error, inspect its remote result again before repeating it.
