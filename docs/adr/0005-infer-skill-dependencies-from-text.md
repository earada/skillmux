# Infer skill dependencies from text, with a manual Suggestion override

## Status

accepted

## Context

Many third-party Skills call other Skills — `grill-with-docs`'s entire body is
"Run a `/grilling` session, using the `/domain-modeling` skill." Install only the
caller and it breaks at runtime: the called Skill isn't in the Target. We want to
warn the user in the matrix so they know what else to install. The Skills are
third-party and uneditable, so we can't ask authors to declare a `dependencies:`
frontmatter field — and existing Skills (the ones with the problem) wouldn't have
it anyway. Inference from the Skill's own content is the only option that helps
today.

## Decision

**Detect** a Dependency from the Skill's text: a `/<name>` invocation token, or a
`../<name>/…` path that crosses into a sibling Skill folder, where `<name>`
matches another Skill's `name` exactly. We scan every text file in the Skill
folder, not just `SKILL.md`. Loose prose mentions (a bare name, no `/`, no
crossing path) are **not** detected — they are the main false-positive source.

**Resolve** by `name`, agnostic to Source: whatever already occupies that name in
the Target satisfies the Dependency (installed reality wins over the catalog).
Dependencies are **transitive** — a Skill needs its whole closure, walked with a
cycle guard. When the only candidate that can satisfy an edge comes from a Source
other than the depending Skill's own, flag the cross-Source resolution.

**Surface** problem-first: a marked/installed cell whose closure is unsatisfied in
that Target turns amber; the matrix is otherwise clean. A cursor detail line lists
`needs:` / `suggests:`. Matrix `d` marks the closure in the cell's Target; the
Plan adds a non-blocking `⚠ broken …` section with `f` to add the missing closure.
Nothing blocks Apply.

**Classify** every detected edge as a Dependency by default; the user downgrades
false ones to a **Suggestion** (recorded in Config as `[[suggestion]]` `from`/`to`,
or `from` alone for a router Skill like `ask-matt`). A Suggestion is inert: shown
in the UI but never warns, never enters the Plan, never joins a closure. Toggled
from the `v` skill view; Config remains hand-editable.

## Considered Options

- **Author-declared frontmatter (`dependencies:`)** — rejected: the Skills are
  third-party and uneditable, so the field would be empty exactly where we need it.
- **Inference from prose mentions** — rejected as detection: `review` names
  `setup-matt-pocock-skills` only as a conditional fallback, and `ask-matt` is a
  router that names a dozen Skills it never needs. The "if missing" / "menu" nuance
  lives in prose and can't be classified mechanically. So we infer only the precise
  signals and let the user reclassify the rest.

## Consequences

- A router like `ask-matt` shows ~12 false amber warnings until its edges are
  downgraded once (persisted). Accepted: the safety net is on by default and curing
  is a one-time exception.
- `/<name>` matching must guard against URLs and incidental paths (`https://h/grilling`)
  — require a word boundary and reject `://` — or false positives creep back in.
