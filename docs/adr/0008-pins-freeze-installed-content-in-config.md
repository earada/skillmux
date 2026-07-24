# A Pin freezes installed content, and lives in the Config

## Status

accepted

## Context

A Source already carries a `branch`, which may be a branch, a tag, or a commit
SHA — so a whole Source can be held at a revision. What was missing is the
per-Skill case: "this one Skill must stop moving, even though I want the rest of
its Source to keep up".

Two things had to be decided: *what* a Pin holds, and *where* it is recorded.

## Decision

**A Pin freezes the content already installed, not a named revision.** The
Manifest's Fingerprint is what that content is, and the installed copy is where
it lives (ADR 0007), so a Pin needs no new state: it simply suppresses the
upstream-drift signal. The cell reads `pinned` instead of `update available`,
and Reconcile skips the catch-up Reinstall.

**A Pin lives in the Config, as a `[[pin]]` entry** scoped by `skill`, by
`source`, or by both — omitting one widens it, the same bulk shape
`[[suggestion]]` already uses. A Pin is a user decision, and the Config is the
user-owned, hand-editable source of truth; the Manifest is machine-managed state
that records what happened, not what the user wants.

**A Pin holds a version; it does not veto the user.** A first Install, an
Uninstall, switching which Source supplies a name, and a re-pointed Target all
still happen. **Modified locally** still wins over Pinned, because local drift is
a fact about reality that a Pin does not make untrue.

## Considered options

- **Pin to a revision (`pin = "a1b2c3d"`) and fetch it** — the literal reading of
  "freeze at a revision", but it needs a second working tree per pinned Skill,
  cannot work for a local Source (no history), and duplicates what a Source's
  `branch` already does at Source scope. Rejected.
- **Record the Pin in the Manifest** — it sits next to the Fingerprint it freezes,
  but it would make a user decision machine-managed: invisible in the file the
  user edits, and lost with any Manifest reset. Rejected.
- **Pin per (Skill, Target) cell** — finer, and expressible, but "frozen here,
  updating there" is a rare want that would double the state a reader of the
  matrix has to hold. A Pin is per row; the whole row shows it. Rejected for now.

## Consequences

- A pinned row hides its own drift on purpose: the matrix shows `◆ pinned` rather
  than `↑ update`, `skillmux check` stays clean, and `skillmux diff` skips it in
  the default walk. `skillmux diff <skill>` (or the TUI's diff screen) is how to
  see what a Pin is holding back — both label it as pinned.
- Pinned is reported whether or not upstream has moved, so a Pin remains visible
  while everything is current. The trade is that the cell no longer says whether
  an update exists; the diff answers that.
- A Pin on a Skill installed nowhere is inert, so the row label carries the glyph
  too — otherwise pinning an uninstalled Skill would show nothing at all.
- A re-pointed Target reinstalls a pinned Skill from the current Source, which may
  differ from the pinned content. The alternative — leaving the new path empty —
  freezes the row into having no files, which is worse.
