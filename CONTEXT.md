# Skillmux

A TUI tool for managing AI-agent skills across the AI coding tools installed on a machine: installing, uninstalling, and detecting drift of skills sourced from GitHub or local folders into multiple agent destinations.

## Language

**Skill**:
A self-contained directory identified by its `SKILL.md` file (frontmatter + auxiliary files). The atomic unit that Skillmux installs, uninstalls, and tracks. Its identity is the `name` from the `SKILL.md` frontmatter, which is also its install folder name in every Target.
_Avoid_: package, plugin, command

**Source**:
An origin that holds one or more Skills — either a GitHub URL (public, or private when a credential is available) or a local folder. A Source may contain N Skills (each subfolder with a `SKILL.md` is one Skill); the user picks which to install.
_Avoid_: repo (when ambiguous), registry

**Revision**:
The exact point in a GitHub Source's history that its local clone currently sits at — surfaced as `ref @ shortSHA` (e.g. `main @ a1b2c3d`). A per-Source fact: every Skill from one clone shares the Source's Revision. Purely informational — it tells the user *what they have*, and is distinct from Update available, which compares per-Skill content fingerprints, not Revisions. Local Sources have no Revision.
_Avoid_: version, commit (in field name), tag

**Group**:
The folder hierarchy a Skill sits under within its Source, derived from where its `SKILL.md` lives (the parent directories, slash-joined). Purely organisational — it is not part of the Skill's identity and does not affect the install folder — but Skillmux surfaces it as a dimmed hint trailing the Skill name in the matrix (`strict-mode  typescript`, name first so the eye lands on identity) and lets the user filter by it. A Skill at the Source root has no Group.
_Avoid_: category, namespace, package

**Deprecated**:
A Skill the author has retired. Skillmux infers this from either signal: the `SKILL.md` frontmatter (`deprecated: true` or `deprecated: "<migration note>"`), or a `deprecated` segment in the Skill's folder path (the convention some Sources use to bucket retired Skills). It still lists the Skill (so existing Installations remain visible) but marks it with a `⊘` glyph and strike-through, gathers all such Skills into the bottom section of the matrix (below a full-width rule), reddens the word "deprecated" in the path, and shows the migration note when the cursor rests on it. Distinct from Update available, which is about drift, not author intent.
_Avoid_: archived, obsolete, retired (in field name)

**Target**:
An AI coding tool installed on the machine that consumes Skills (e.g. Claude Code, Cursor, Codex). Each Target is a configurable `{ name, path }` where `path` is the directory Skillmux installs Skills into. In v1 all Targets share a homogeneous skill format (a folder containing `SKILL.md`); installing = copying the Skill folder into the Target's path.
_Avoid_: agent (collides with Claude Code "subagent"), host, destination

**Candidate**:
A known AI tool detected on the machine but not yet configured as a Target — a ready-to-add `{name, path}` proposal shown in the config screen's **found** section. Detection is by the tool's root directory existing (e.g. `~/.claude` → `claude-code` at `~/.claude/skills`); the skills folder itself may not exist yet, installing creates it. A Candidate is suppressed while any configured Target covers it (same name or same expanded path — the user already made their call), and returns if that Target is deleted. Adopting one (`a`, or `e` to tweak first) just adds a Target; nothing else changes until Apply.
_Avoid_: detected target (as a noun), suggestion (collides with Suggestion)

**Installation**:
The fact of a specific Skill being present in a specific Target — the (Skill, Target) pair. Skillmux records, per Installation, which version of the Skill was installed so it can later detect upstream drift.
_Avoid_: deployment, copy

**Update available**:
The status of an Installation whose Source has changed since the Skill was last installed into that Target (upstream drift). The primary signal Skillmux surfaces; it prompts a reinstall to catch up. Distinct from Modified locally, which is drift of the installed copy itself.
_Avoid_: outdated, stale, dirty

**Modified locally**:
The status of an Installation whose installed copy no longer matches the Fingerprint recorded at install time — the user (or another tool) edited or removed it by hand. The counterpart of Update available: local drift instead of upstream drift, and it wins when both are present, because a reinstall would discard the user's changes. Apply requires an explicit confirmation (like the untracked-folder overwrite of ADR 0002) before overwriting a modified copy; the headless CLI never overwrites one — it skips the operation with a note and leaves the decision to the TUI. A missing copy is surfaced as Modified locally too (reality diverged from the Manifest), but restoring it needs no confirmation since there are no edits to lose.
_Avoid_: dirty, tampered, local drift (in field name)

**Upstream diff**:
The file-level comparison Skillmux shows before a reinstall, so an Update available says *what* changed rather than only *that* something did: the files added, removed and modified, plus the unified line hunks of each text file. The old side is the copy installed in the Target — which is exactly the content the Manifest Fingerprint attests to — and the new side is the Skill's current folder in the Source cache (ADR 0007). It is reachable from the skill explorer (compared against the Target column the matrix cursor sits on) and from the Plan (against the operation under the cursor) — the two places a reinstall is decided — and headless via `skillmux diff`, which prints the same comparison as plain unified text for CI and dotfiles. When the installed copy is Modified locally — or is an untracked folder Skillmux never installed — the comparison is still shown but flagged, because the old side then carries the user's own edits and the changes read as "installed vs upstream" rather than purely upstream.
_Avoid_: patch, changeset, delta

**Apply**:
The single reconciliation step that brings reality in line with the user's desired selection: installs Skills marked for a Target that aren't there, uninstalls those unmarked that are, and reinstalls those with an Update available. The TUI edits desired state; nothing changes until Apply, which first shows a confirmable plan and then runs best-effort (per-operation result, no global rollback).
_Avoid_: sync, commit, run

**Plan**:
The preview shown before Apply runs: the concrete list of install / uninstall / reinstall operations the reconciliation will perform, presented for confirmation.
_Avoid_: diff, changeset

**Conflict**:
The situation where two Skills from different Sources share the same `name`, and thus the same install folder, in a single Target. They cannot coexist; Skillmux surfaces the Conflict and the user picks which Skill wins.
_Avoid_: clash, collision (in prose only)

**Dependency**:
A directed relationship where one Skill needs another Skill present in the same Target to work — the depended-on Skill is identified by `name`. Skillmux infers it from the Skill's own text: a `/<name>` invocation token, or a `../<name>/…` path that crosses into a sibling Skill folder, where `<name>` exactly matches another Skill's `name` in the catalog. Loose prose mentions (a name without the `/` or a crossing path) are deliberately not Dependencies — they produce false positives. Detection scans every text file in the Skill's folder, not just `SKILL.md`. Dependencies are transitive: a Skill needs the whole closure of what it (and its Dependencies) reach, walked with a cycle guard. A Dependency is Unsatisfied for a given Target when the Skill is installed (or marked) there but some Skill in its closure is not. Resolution is by `name`, agnostic to which Source supplies it: whatever already occupies that name in the Target satisfies the Dependency (installed reality wins over the catalog). When the only candidate that can satisfy it comes from a Source other than the depending Skill's own, Skillmux flags the cross-Source resolution, since a same-named Skill from elsewhere may not be the companion the author intended. Every detected edge is a Dependency by default; the user may reclassify an edge as a Suggestion.
_Avoid_: requirement, link, reference (in field name)

**Suggestion**:
A soft, optional edge from one Skill to another — "you might also want X" — as opposed to a Dependency. It exists only because inference cannot tell a hard call from an advisory mention (e.g. `review` points at `setup-matt-pocock-skills` only _if_ a file is missing; `ask-matt` is a router that names a dozen Skills it never needs). Detection cannot distinguish the two, so every detected edge defaults to Dependency and the user downgrades the false ones to Suggestion. A Suggestion is recorded by the user in Config (a `[[suggestion]]` `from`/`to` pair, or `from` alone to mark every outgoing edge of a router Skill). It is inert: it is shown in the UI (distinct from a Dependency, never the warning colour) but never marks a cell Unsatisfied, never enters the Plan, and is never pulled into a Dependency's closure.
_Avoid_: recommendation, hint, optional dependency

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
