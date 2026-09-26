# Issue tracker: GitHub

Issues and specs live in GitHub Issues for `agisilaos/acal`.
Use the `gh` CLI, with `--repo agisilaos/acal` when repository
context is ambiguous.

## Operations

- Read: `gh issue view <number> --comments`; include labels when triaging.
- List: `gh issue list --state open --json number,title,body,labels,comments`.
- Create: `gh issue create --title "<title>" --body-file <file>`.
- Comment: `gh issue comment <number> --body-file <file>`.
- Label: `gh issue edit <number> --add-label "<label>"` or `--remove-label`.
- Close: `gh issue close <number>`.

Write multiline bodies to a file and pass `--body-file`.

“Publish to the issue tracker” means create a GitHub issue.
“Fetch the relevant ticket” means read the issue and its comments.
GitHub shares issue and PR numbering; resolve the object type when unclear.

## Pull requests as a triage surface

**PRs as a request surface: no.**

## Wayfinding

Use one issue labelled `wayfinder:map` for Notes, Decisions-so-far,
and Fog. Link child tickets as GitHub sub-issues, or use a task list
and `Part of #<map>` when sub-issues are unavailable.

Child labels are `wayfinder:research`, `wayfinder:prototype`,
`wayfinder:grilling`, or `wayfinder:task`.

Record blockers using native issue dependencies. When unavailable,
use `Blocked by: #<number>` in the child body. Select the first open,
unassigned child in map order whose blockers are all closed.

Claim with `gh issue edit <number> --add-assignee @me`.
Resolve by recording the answer, closing the ticket, and adding
a summary and link to the map's Decisions-so-far.
