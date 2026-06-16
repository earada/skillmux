# Skillmux

A TUI tool for managing AI-agent skills across the AI coding tools installed on a machine: installing, uninstalling, and detecting drift of skills sourced from GitHub or local folders into multiple agent destinations.

## Language

**Skill**:
A self-contained directory identified by its `SKILL.md` file (frontmatter + auxiliary files). The atomic unit that Skillmux installs, uninstalls, and tracks. Its identity is the `name` from the `SKILL.md` frontmatter, which is also its install folder name in every Target.
_Avoid_: package, plugin, command

**Source**:
An origin that holds one or more Skills — either a public GitHub URL or a local folder. A Source may contain N Skills (each subfolder with a `SKILL.md` is one Skill); the user picks which to install.
_Avoid_: repo (when ambiguous), registry

**Group**:
The folder hierarchy a Skill sits under within its Source, derived from where its `SKILL.md` lives (the parent directories, slash-joined). Purely organisational — it is not part of the Skill's identity and does not affect the install folder — but Skillmux surfaces it as a dimmed hint trailing the Skill name in the matrix (`strict-mode  typescript`, name first so the eye lands on identity) and lets the user filter by it. A Skill at the Source root has no Group.
_Avoid_: category, namespace, package

**Deprecated**:
A Skill the author has retired. Skillmux infers this from either signal: the `SKILL.md` frontmatter (`deprecated: true` or `deprecated: "<migration note>"`), or a `deprecated` segment in the Skill's folder path (the convention some Sources use to bucket retired Skills). It still lists the Skill (so existing Installations remain visible) but marks it with a `⊘` glyph and strike-through, gathers all such Skills into the bottom section of the matrix (below a full-width rule), reddens the word "deprecated" in the path, and shows the migration note when the cursor rests on it. Distinct from Update available, which is about drift, not author intent.
_Avoid_: archived, obsolete, retired (in field name)

**Target**:
An AI coding tool installed on the machine that consumes Skills (e.g. Claude Code, Cursor, Codex). Each Target is a configurable `{ name, path }` where `path` is the directory Skillmux installs Skills into. In v1 all Targets share a homogeneous skill format (a folder containing `SKILL.md`); installing = copying the Skill folder into the Target's path.
_Avoid_: agent (collides with Claude Code "subagent"), host, destination

**Installation**:
The fact of a specific Skill being present in a specific Target — the (Skill, Target) pair. Skillmux records, per Installation, which version of the Skill was installed so it can later detect upstream drift.
_Avoid_: deployment, copy

**Update available**:
The status of an Installation whose Source has changed since the Skill was last installed into that Target (upstream drift). The primary signal Skillmux surfaces; it prompts a reinstall to catch up. Distinct from local modification of the installed copy, which is out of scope for v1.
_Avoid_: outdated, stale, dirty

**Apply**:
The single reconciliation step that brings reality in line with the user's desired selection: installs Skills marked for a Target that aren't there, uninstalls those unmarked that are, and reinstalls those with an Update available. The TUI edits desired state; nothing changes until Apply, which first shows a confirmable plan and then runs best-effort (per-operation result, no global rollback).
_Avoid_: sync, commit, run

**Plan**:
The preview shown before Apply runs: the concrete list of install / uninstall / reinstall operations the reconciliation will perform, presented for confirmation.
_Avoid_: diff, changeset

**Conflict**:
The situation where two Skills from different Sources share the same `name`, and thus the same install folder, in a single Target. They cannot coexist; Skillmux surfaces the Conflict and the user picks which Skill wins.
_Avoid_: clash, collision (in prose only)

**Config**:
The user-owned, hand-editable declarative input (TOML, under XDG `~/.config/skillmux/`): the list of Targets and the list of Sources (each `{ url-or-folder, branch?, subpath? }`). Also editable from the TUI, but the file is the readable source of truth.
_Avoid_: settings, manifest

**Manifest**:
The Skillmux-owned, machine-managed state record (not hand-edited): the set of Installations and their recorded version fingerprints, used to detect Update available. Distinct from Config.
_Avoid_: lockfile (in prose), state, db

## Example dialogue

> **Dev:** When I open the TUI, where does the list of skills come from?
> **Domain expert:** From the Sources in your Config. Each Source — a GitHub repo or a local folder — is scanned recursively, and every directory with a `SKILL.md` is one Skill.
> **Dev:** And the columns are the agents?
> **Domain expert:** Call them Targets, not agents — "agent" means a subagent in Claude Code. A Target is just a `{name, path}` you configured. The grid is Skills × Targets.
> **Dev:** A cell says "update available". What changed?
> **Domain expert:** The Source changed since you last installed that Skill into that Target — upstream drift. We hash the Source folder and compare it to the fingerprint in the Manifest. If you'd hand-edited the installed copy instead, we wouldn't flag that — local drift is out of scope.
> **Dev:** So I tick some cells and it installs?
> **Domain expert:** Not yet. You're editing desired state. When you hit Apply it shows a Plan — what it'll install, uninstall, reinstall — and only after you confirm does it touch disk. And it'll refuse to clobber a `deploy/` folder it didn't put there: that's a Conflict, you resolve it explicitly.
