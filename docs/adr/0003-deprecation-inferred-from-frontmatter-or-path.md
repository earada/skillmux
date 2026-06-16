# Deprecation inferred from frontmatter or folder path

Skillmux treats a Skill as **Deprecated** when *either* signal is present: its `SKILL.md` frontmatter declares it (`deprecated: true` or `deprecated: "<note>"`), *or* the Skill's folder path within its Source contains a `deprecated` segment (case-insensitive). Both feed one predicate that drives the same treatment — the Skill sinks into the bottom section of the matrix, is struck through with a `⊘` glyph, and the word "deprecated" is reddened where it appears in the path. We accept the path heuristic because real-world Sources (e.g. `mattpocock/skills`) bucket retired Skills under a `deprecated/` folder without touching each `SKILL.md`; honouring that convention surfaces them correctly with zero author effort, while the frontmatter field stays the explicit, portable signal for authors who want one (and the only one that can carry a migration note).

## Consequences

- A Skill placed under any path segment matching "deprecated" is flagged even if that was incidental naming; the convention is treated as intent. The frontmatter field remains available to flag a Skill whose path says nothing.
- The two signals are unified in the TUI presentation layer (`isDeprecated`), not at scan time, so `domain.Skill.Deprecated` keeps meaning strictly "the author declared it in frontmatter" — the path signal does not rewrite that field.
- Only the frontmatter form carries a reason; a path-deprecated Skill shows "deprecated" with no migration note.
