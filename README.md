# skillmux

A TUI for managing AI-agent **Skills** across the AI coding tools (**Targets**)
installed on your machine — installing, uninstalling, and detecting when a
Skill's upstream **Source** has changed since you last installed it.

- **Skill** — a self-contained directory identified by its `SKILL.md`.
- **Source** — a public GitHub repo or a local folder holding one or more Skills.
- **Target** — an AI tool that consumes Skills, configured as `{ name, path }`.
- **Group** — the folder hierarchy a Skill sits under within its Source, shown
  as a dimmed hint after the name.
- **Deprecated** — a retired Skill, flagged by its `SKILL.md` frontmatter or by a
  `deprecated/` folder in its path; gathered at the bottom of the matrix.

You edit a desired Skills × Targets selection in the TUI; **Apply** reconciles
reality to match it (install / uninstall / reinstall) after showing a confirmable
**Plan**.

See [`CONTEXT.md`](./CONTEXT.md) for the full glossary and
[`docs/adr/`](./docs/adr/) for the key design decisions.

## Status

Functional v1 end-to-end: scan sources, detect upstream drift, reconcile a
desired selection, and apply it from a Bubble Tea matrix. The matrix renders
instantly on startup from the last cached catalog while a fresh scan runs in
the background.

Rows are organised to surface what matters: skills split into three sections —
**installed** (in at least one target), **not installed**, then **deprecated** —
separated by full-width rules, and within each section grouped by source then
sorted by name so a source's skills stay together. Each row leads with the skill
name (accent-coloured, or red when the same name comes from more than one
source) followed by its folder **group** as a dimmed hint; a skill is treated as
**deprecated** when its `SKILL.md` says so or its path contains a `deprecated/`
folder, shown struck-through with a `⊘` glyph (and the word "deprecated" reddened
in the path).

When the same skill name is offered by more than one source, selection is
exclusive per target — choosing one source deselects the others, so you pick the
winner instead of hitting a conflict. Targets are configured by hand (no
auto-detection yet).

## Configuration

`~/.config/skillmux/config.toml`:

```toml
[[target]]
name = "claude-code"
path = "~/.claude/skills"

[[source]]
name = "my-skills"
location = "https://github.com/owner/repo"
branch  = "main"     # optional; default branch when omitted
subpath = "skills"   # optional; narrows where skills are scanned

[[source]]
name = "local"
location = "~/dev/skills"
```

### GitHub repos

Skillmux fetches GitHub Sources with `git`, kept as a shallow clone under the
cache and updated in place — so **git must be installed** (a GitHub Source on a
machine without git fails fast; local Sources still work). Authentication is
left to git: private repos work through your own credential helper or SSH keys,
and an `git@github.com:owner/repo` SSH location clones directly — Skillmux never
reads or stores a token. Only `github.com` is in scope. See
[ADR 0006](./docs/adr/0006-fetch-github-sources-via-git-clone.md).

Run `skillmux` to open the matrix. Keys: arrows move · `space` toggle a cell ·
`a` all targets for a skill · `n` none · `/` filter skills by name, group or
source (vim-style, `esc` clears) · `v` view a skill · `r` refresh · `p` preview
the plan · `c`
manage targets/sources ·
`q` quit. From the plan, `y` applies. `v` opens a read-only explorer for the
skill under the cursor — metadata (including the source's `ref @ commit` for a
GitHub clone) plus a navigable file tree; `enter` on a file shows its contents
(markdown rendered, anything else raw), `esc` steps back. The config
screen (`c`) lists sources then targets (split by a rule), showing each GitHub
source's current `ref @ commit` and when it was last fetched, and adds (`t`/`s`),
edits (`e`) and deletes (`d`) them, writing changes back to `config.toml`; you
can still edit the file by hand. `C` clears the download cache of the source
under the cursor (a no-op for local sources), so the next refresh re-downloads
it from scratch. If applying would overwrite a folder
skillmux didn't install, it lists those folders and asks you to confirm
(`y` adopts them, `n` cancels) before touching them — see ADR 0002.

## Layout

```
cmd/skillmux        entrypoint
internal/domain     core types (Skill, Source, Target, Installation, Status)
internal/config     user-owned Config (TOML, XDG ~/.config/skillmux)
internal/manifest   Skillmux-owned Manifest (JSON, XDG ~/.local/state/skillmux)
internal/paths      XDG path resolution
internal/source     recursive Skill discovery (SKILL.md frontmatter: name, description, deprecated; group from path)
internal/fingerprint deterministic content hash of a Skill folder
internal/fetch      resolve a Source (local folder / shallow git clone in cache)
internal/reconcile  desired selection -> Plan (pure)
internal/apply      execute a Plan against disk (best-effort, safe)
internal/engine     orchestration: Refresh / Status / Plan / Apply
internal/tui        Bubble Tea matrix front-end
```
