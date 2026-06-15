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

Early development. Domain model, Config (TOML) and Manifest (JSON) persistence
are in place; the Source scanning / hashing / reconcile core and the Bubble Tea
TUI are next.

## Layout

```
cmd/skillmux        entrypoint
internal/domain     core types (Skill, Source, Target, Installation, Status)
internal/config     user-owned Config (TOML, XDG ~/.config/skillmux)
internal/manifest   Skillmux-owned Manifest (JSON, XDG ~/.local/state/skillmux)
internal/paths      XDG path resolution
```
