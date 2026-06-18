---
status: superseded by ADR-0006
---

# Private GitHub repos via token over HTTPS, not git

> **Superseded by [ADR 0006](./0006-fetch-github-sources-via-git-clone.md).**
> Skillmux now clones with `git` and defers auth to git's own credential
> resolution; the tarball-over-HTTPS path and ambient-token handling described
> below have been removed. Kept for historical context.

To support private Sources we keep the existing "download a tarball over HTTPS, no `git` dependency" architecture and add an ambient credential rather than shelling out to `git clone` / SSH. When a token is available, Skillmux fetches `https://api.github.com/repos/{owner}/{repo}/tarball/{ref}` with an `Authorization: Bearer` header (the API redirects to a signed `codeload` URL that Go follows); when no token is available it stays on the anonymous `codeload.github.com` path exactly as before. The token is resolved ambiently — `GH_TOKEN` → `GITHUB_TOKEN` → `gh auth token` — never stored in the Config, never written to disk, and never echoed in logs or errors.

## Considered options

- **`git clone` over SSH / HTTPS credential helper** — would reuse the user's `~/.ssh` and credential store, but introduces a hard dependency on `git` being installed, in direct tension with the project's deliberate "no git dependency" stance. Rejected.
- **A `token` field per Source in `config.toml`** — explicit and per-repo, but puts a secret in plaintext under `~/.config`. Rejected in favour of an ambient credential.
- **A per-Source `private` flag** — unnecessary: the token (when present) is attached to every GitHub fetch and works for public and private repos alike, so privacy is detected implicitly. No config change at all.

## Consequences

- Two endpoints now exist for GitHub Sources (anonymous `codeload` vs. authenticated `api.github.com` tarball). A future reader should not "simplify" them into one — the split is what lets unauthenticated fetches avoid the API's 60 req/h limit while authenticated fetches reach private repos.
- Only `github.com` is supported. GitHub Enterprise (self-hosted `api/v3` hosts, separate tokens) is an explicit non-goal for now; it would require detecting the host from the Source `Location`.
- A `404` on a GitHub Source is ambiguous (missing vs. private-without-access), so error messages are tailored by whether a token was present, pointing the user at `GH_TOKEN`/`GITHUB_TOKEN`/`gh auth login`.
