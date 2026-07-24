# The installed copy is the diff's old side

## Status

accepted

## Context

**Update available** is computed from content hashes (ADR 0001): a fingerprint
mismatch proves *that* a Skill changed upstream, but says nothing about *what*
changed. Reinstalling is therefore a blind decision — the user is told to catch
up without being shown what catching up brings in.

Showing a real diff needs the bytes of both sides. The Manifest keeps only a
hash, and Skillmux keeps no historical snapshot of a Source: the cache holds a
single working tree at the current Revision (a shallow clone for GitHub, and for
a local folder no history exists at all).

## Decision

Diff the **copy installed in the Target** (old side) against the Skill's
**current folder in the Source cache** (new side).

The installed copy *is* the content recorded at install time — that is precisely
what the Manifest Fingerprint attests to — so it doubles as the "before"
snapshot and no extra state has to be stored. The comparison is still offered
when that attestation does not hold (a **Modified locally** copy, or an
untracked folder Apply has not adopted), but it is labelled: the old side then
carries the user's own edits, so the changes read as "installed vs upstream"
rather than purely upstream.

## Considered options

- **Keep a pristine copy of every Installation in the state directory** — gives a
  true upstream-only diff even for a hand-edited installation, but doubles the
  disk cost of every Skill to buy a strictly better answer only in the case where
  we already warn the user that reality diverged. Rejected.
- **Diff two git revisions of the clone** — excludes local Sources entirely, needs
  history a shallow clone does not have, and compares the wrong thing: ADR 0001
  deliberately keyed drift to content hashes rather than commits, so a Revision
  range is not what the Manifest recorded. Rejected.
- **Record a per-file hash map in the Manifest** — would let Skillmux name the
  changed files without the Source's content, but still could not show a line
  diff, and it grows the Manifest format for half an answer. Rejected.

## Consequences

- The diff is unavailable in two shapes, both reported inline rather than as an
  empty screen: no copy installed in that Target (no old side) and a Skill
  removed upstream (no new side).
- A locally modified copy yields a mixed diff. It is labelled, not suppressed —
  seeing the combined change is still the best available basis for deciding
  whether to discard the local edits.
- The diff's notion of "Skill content" must track the fingerprint's (regular
  files only, symlinks and specials skipped). If the two drifted apart, a cell
  flagged as drifted could render an empty diff, or an identical folder could
  render a change.
