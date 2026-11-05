# Starting and landing an acal session

Use a separate worktree and branch for each implementation chat. GitHub issues hold the
spec and decisions. Keep generated evidence outside the checkout.

The primary checkout must contain its own `.git` directory.

## Start

1. Read the issue and its comments, check related work, and run `scripts/session list`.
   For overlapping worktrees, inspect their status and changes against their own base
   before choosing which files to edit.
2. Run `scripts/session start <name> [--issue <number>]`. It fetches the latest
   `origin/main`, creates a sibling checkout named `acal-<name>` with branch
   `fix/<name>`, and prints the path. Use `--issue` for a tracked task. It checks
   that the issue is open and reserves it among worktrees sharing this clone.
3. Start the new Claude or Codex chat in that checkout. Give it a short brief with the
   issue, goal, scope, settled decisions, relevant evidence, acceptance checks, and
   authorization to investigate, implement, commit, or land as applicable. Ask the
   recipient to refresh the issue and ownership before editing.

## Work

Keep changes in the session worktree. Run focused checks while editing and stage
owned files by explicit path. Keep unrelated edits in their owner's worktree. To
set work aside, use a patch outside the checkout. Git's stash is shared by all worktrees.

Report what changed, what passed, and what still needs a decision or verification.
Reconcile relevant docs and issue state with the result. Commit when authorized.

## Land

When authorized to push, use `scripts/session land --worktree <path>` from a clean,
current main checkout. The main checkout supplies the driver, including when the
task changes `scripts/session`. A landing requires a clean worktree on its original task branch,
linear commits in a full clone, Git 2.46 or newer, and one `origin` URL shared by fetch and push.
For HTTPS authentication, use a Git credential helper and keep credentials out of
the URL.

For the first installation, check out the reviewed driver on `fix/<name>` in a sibling
worktree named `acal-<name>` based on the latest `origin/main`. Run
`scripts/session land --bootstrap` there. Bootstrap is available until GitHub's
`main` contains the driver.

1. Keep the `origin/main` tracking reflog intact. Use the script for rebases onto
   `origin/main`. Local preparation branches based on the same upstream base are
   supported. If Git stops on a conflict,
   preserve that worktree for inspection and reconcile the task in a fresh session
   based on current `main`. If `main` was rolled back past the recorded base, compare
   the task with the rollback before starting fresh. In either case, run
   `scripts/session release-issue <old-name>` from the main checkout before starting
   a retry with the same `--issue`. This preserves the old checkout and prevents it
   from landing later.
2. A read-only Codex run starts outside the candidate checkout and reviews each commit
   in the exact range. Findings and incomplete reviews stop the landing. Address
   actionable findings in scope, then retry with the corrected commits. The script
   uses the desktop app's bundled CLI when installed
   in `/Applications`, or `codex` on `PATH`. Set `ACAL_CODEX` to an absolute path
   to select another executable.
3. After review it rechecks the worktree, branch and remote base, runs `make verify`
   on a temporary checkout of the reviewed commit, then checks the candidate and
   remote again. A shared lock serializes Git operations and verification within
   this clone. Other sessions can proceed while Codex reviews.
4. With both gates passing, it pushes the reviewed commit directly to `origin/main`
   as a fast-forward, without a pull request. The push requires the remote to
   still point at the reviewed base. The script then verifies the commit on the remote.

If `main` or the candidate moves during the gates, run `land` again to qualify the
new range. The gates live in `scripts/session land`. Use that command for task landings.
`scripts/session land --worktree <path> --dry-run` rebases, reviews, verifies, and
rehearses the push without running pre-push hooks or publishing. Each attempt prints log paths under
`~/Library/Logs/acal/sessions/`.

## End

Record durable lessons in the relevant guide and leave unfinished work on its issue.
From the main checkout, run `scripts/session end <name>` when cleanup is requested.
Finish builds and other commands in the session before ending it.
It checks that the named worktree is clean, including local ignored files, and that its
commits are on the latest `origin/main` before removing the worktree and its branch.
Preserve sessions with initialized submodule metadata for manual cleanup.
If branch removal stops after the worktree is gone, rerun `end` to finish cleanup.
