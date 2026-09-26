# Domain docs

This repository uses a single-context layout:

- `CONTEXT.md`: domain vocabulary and model.
- `docs/adr/`: architectural decisions.
- `docs/architecture.md`: implementation ownership and boundaries.

Before domain exploration, read `CONTEXT.md` and relevant ADRs.
For backend, persistence, output, or architecture changes, also read
`docs/architecture.md`.

If CONTEXT.md or ADRs are absent, proceed silently. Domain-modeling
work creates them when vocabulary or decisions are resolved; this
setup does not create placeholders.

Use established glossary terms in issues, proposals, and code.
When a needed concept is missing, distinguish an unnecessary synonym
from a genuine gap to resolve through domain modeling.

Surface conflicts with an existing ADR explicitly, identifying the
decision and why it may need reopening.
