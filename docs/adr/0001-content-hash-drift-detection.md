# Drift detection by content-hash fingerprint, upstream-only

Skillmux detects an **Update available** by comparing a content hash (e.g. SHA-256) of the Source's current Skill folder against the fingerprint recorded for that Installation in the central Manifest. We chose a content hash over the git commit SHA so GitHub and local-folder Sources are treated identically (no dependency on git, no special case per Source type); checking a GitHub Source therefore requires a network fetch of its contents. We scope v1 to **upstream drift only** — local modification of the installed copy at a Target is out of scope — to keep the model simple and the action unambiguous (an Update available always means "reinstall to catch up").

## Consequences

- The fingerprint format is baked into the Manifest; changing it later invalidates existing recorded state.
- Update checks for GitHub Sources cost network I/O (mitigated by background fetching + a local cache).
