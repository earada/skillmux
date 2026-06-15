# skillmux

A TUI for managing AI-agent **Skills** across the AI coding tools (**Targets**)
installed on your machine — installing, uninstalling, and detecting when a
Skill's upstream **Source** has changed since you last installed it.

- **Skill** — a self-contained directory identified by its `SKILL.md`.
- **Source** — a public GitHub repo or a local folder holding one or more Skills.
- **Target** — an AI tool that consumes Skills, configured as `{ name, path }`.

You edit a desired Skills × Targets selection in the TUI; **Apply** reconciles
reality to match it (install / uninstall / reinstall) after showing a confirmable
**Plan**.

See [`CONTEXT.md`](./CONTEXT.md) for the full glossary and
[`docs/adr/`](./docs/adr/) for the key design decisions.

## Status

Functional v1 end-to-end: scan sources, detect upstream drift, reconcile a
desired selection, and apply it from a Bubble Tea matrix. The matrix renders
instantly on startup from the last cached catalog while a fresh scan runs in
the background. When the same skill name is offered by more than one source
(marked ⚠), selection is exclusive per target — choosing one source deselects
the others, so you pick the winner instead of hitting a conflict. Targets are
configured by hand (no auto-detection yet).

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

Run `skillmux` to open the matrix. Keys: arrows move · `space` toggle a cell ·
`a` all targets for a skill · `n` none · `r` refresh · `p` preview the plan ·
`c` manage targets/sources · `q` quit. From the plan, `y` applies. The config
screen (`c`) adds (`t`/`s`) and deletes (`d`) targets and sources and writes
them back to `config.toml`; you can still edit the file by hand. If applying would overwrite a folder
skillmux didn't install, it lists those folders and asks you to confirm
(`y` adopts them, `n` cancels) before touching them — see ADR 0002.

## Layout

```
cmd/skillmux        entrypoint
internal/domain     core types (Skill, Source, Target, Installation, Status)
internal/config     user-owned Config (TOML, XDG ~/.config/skillmux)
internal/manifest   Skillmux-owned Manifest (JSON, XDG ~/.local/state/skillmux)
internal/paths      XDG path resolution
internal/source     recursive Skill discovery (SKILL.md frontmatter)
internal/fingerprint deterministic content hash of a Skill folder
internal/fetch      resolve a Source (local folder / GitHub tarball + cache)
internal/reconcile  desired selection -> Plan (pure)
internal/apply      execute a Plan against disk (best-effort, safe)
internal/engine     orchestration: Refresh / Status / Plan / Apply
internal/tui        Bubble Tea matrix front-end
```
