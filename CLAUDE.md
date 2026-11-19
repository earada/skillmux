# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->


## Build & Test

Go module (`go 1.25`), no code generation, no Makefile. `git` must be on PATH
for the `internal/fetch` and `internal/engine` tests that build local repos.

```bash
go build ./...                     # compile everything
go run ./cmd/skillmux              # open the TUI (or: status | check | diff | apply | version)
gofmt -l cmd internal              # must print nothing
go vet ./...
go test -race -count=1 ./...       # full suite; internal/fetch takes ~35s (timeout tests)

# Static analysis, run without installing:
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Quality gate before a commit: `gofmt`, `go vet` and `go test -race` all clean.

## Architecture Overview

A Bubble Tea TUI (plus a headless CLI) that installs AI-agent **Skills** from
**Sources** into **Targets**. Vocabulary lives in `CONTEXT.md`; design
decisions in `docs/adr/`. Read both before changing behaviour.

Everything is under `internal/`, layered bottom-up:

- `domain` — plain types (Skill, Source, Target, Installation, Status). No logic.
- `paths` — XDG locations: Config in `~/.config/skillmux`, Manifest in
  `~/.local/state/skillmux`, clones and the catalog cache in `~/.cache/skillmux`.
- `config` — user-owned `config.toml` (targets, sources, suggestions, pins).
  `Save` is atomic and preserves hand-written comments (`preserve.go`).
- `manifest` — skillmux-owned `manifest.json`: the recorded Installations and
  their fingerprints.
- `fetch` — resolves a Source to a directory; GitHub Sources are shallow git
  clones driven by shelling out to `git` (validated, time-bounded, non-interactive).
- `source` — scans a directory for `SKILL.md` Skills and their cross-references.
- `fingerprint` — content hash of a Skill folder; the basis of drift detection.
- `reconcile` — pure function: desired selection + installed state → Plan.
- `apply` — executes a Plan on disk (staged copy + atomic swap) and enforces
  ADR 0002: never overwrite or delete what skillmux does not manage, or what
  the user edited, without confirmation.
- `diff` — file and line diff between an installed copy and its Source.
- `engine` — the seam the UIs talk to: Refresh, Status, Preview, Apply, and the
  Config mutations. `opMu` serialises everything that touches disk or shared state.
- `cli` — headless `status` / `check` / `diff` / `apply` / `version`.
- `tui` — the Bubble Tea model. Refresh and Apply run as background commands;
  the UI loop must never call an Engine method that takes `opMu` while `busy()`.
- `safetext`, `detect` — terminal-safe rendering of untrusted text; detection of
  installed AI tools.

## Conventions & Patterns

- Use the domain terms from `CONTEXT.md` exactly (its "_Avoid_" lists name the
  synonyms not to use). Record a decision that changes behaviour as an ADR.
- Test first. Tests sit next to the code, one topic per `*_test.go` file
  (`pin_test.go`, `modified_test.go`, …); engine tests build on `newEnv` in
  `internal/engine/engine_test.go`. Fault injection goes through package-level
  seams (e.g. `copyTree`, `renamePath` in `apply`), not special filesystem states.
- Anything read from a Source or a Target is untrusted: validate names before
  they become paths (`source.validateSkillName`, `apply.destWithin`), validate
  what reaches git (`fetch.ValidateSource`), and print through `safetext`.
- Writes to user or state files are atomic (temp file in the same directory,
  then rename). A failed mutation rolls the in-memory Config back.
- A preview and its enforcement share one predicate (`collides`,
  `modifiedOverwrite`), so what the Plan shows and what Apply does cannot drift.
- Comments explain why, and cite the issue (`skillmux-xxx`) or ADR behind a
  non-obvious choice. Commit subjects follow `type(scope): summary (skillmux-xxx)`.
