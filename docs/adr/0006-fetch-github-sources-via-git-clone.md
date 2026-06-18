---
status: accepted
---

# Fetch GitHub Sources via git clone, deferring auth to git

Skillmux now fetches a GitHub Source by maintaining a shallow, single-branch
`git clone` under `~/.cache/skillmux/github/<source>` and updating it with
`git fetch` + `reset --hard` on Refresh, rather than downloading and
re-extracting a tarball every time. This makes Refresh cheap when nothing
changed, gives us the exact commit (`git rev-parse HEAD`) to surface in the UI,
and turns the cache into a durable working copy instead of a disposable one.
This **supersedes [ADR 0004](./0004-private-repos-via-token-over-https.md)**,
whose entire premise — "no `git` dependency" — we are deliberately abandoning.

## Considered options

- **Keep the tarball-over-HTTPS path (ADR 0004) and only make it durable** —
  stop wiping the cache, store the resolved commit, skip re-download when
  unchanged. Avoids the git dependency, but the anonymous codeload tarball does
  not expose the commit SHA (its top dir is `repo-HEAD`, its ETag is a tarball
  hash), so "what commit am I on" would cost an extra rate-limited GitHub API
  call. Rejected: git gives the commit, incremental fetch, and atomic-ish
  updates for free.
- **git clone, but keep ambient token resolution** (`GH_TOKEN` →
  `GITHUB_TOKEN` → `gh auth token`) injected per-command via
  `git -c http.extraHeader`. Preserves today's behaviour, but keeps token-
  handling code we no longer need. Rejected in favour of deferring entirely to
  git's own credential resolution.

## Decisions

- **git is now a hard requirement.** If `git` is not on `PATH` (or a clone
  fails), Skillmux fails fast with an actionable error. There is no tarball
  fallback — the codeload / `api.github.com` tarball / ambient-token machinery
  is deleted.
- **Authentication is deferred to git.** Skillmux no longer reads
  `GH_TOKEN`/`GITHUB_TOKEN`/`gh auth token`. Private Sources authenticate
  through the user's own git credential helper or SSH keys; an
  `git@github.com:owner/repo` SSH `Location` now works directly. This reuses
  exactly the credential-helper/SSH approach ADR 0004 rejected — that rejection
  was justified only by the no-git stance, which no longer holds.
- **Clones are shallow and single-branch.** `git clone --depth 1
  --single-branch [--branch <ref>]`, updated with `git fetch --depth 1` +
  `reset --hard origin/<ref>`; re-clone only when the pinned ref changes.

## Consequences

- A machine without git can no longer use GitHub Sources at all (local Sources
  still work). This is the cost of the simpler, single-path fetch.
- The background Refresh now rewrites a working tree in place. To avoid tearing
  reads under the file explorer, Refresh runs `git fetch` (objects only)
  unconditionally but **defers the `reset --hard` checkout for a Source while a
  skill view for it is open**, applying it when the view closes.
- Drift detection is unchanged: "Update available" still compares the per-Skill
  content fingerprint ([ADR 0001](./0001-content-hash-drift-detection.md)). The
  commit SHA is display-only — switching drift to the commit would coarsely
  flag every Skill in a repo whenever any file changed.
- Only `github.com` over git remains in scope; GitHub Enterprise is still a
  non-goal.
