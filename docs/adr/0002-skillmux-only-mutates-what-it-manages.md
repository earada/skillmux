# Skillmux only mutates what it manages

On Apply, Skillmux will never destroy data it did not create. Uninstall removes **only** Installations recorded in the Manifest; it never deletes a Target folder it does not track. Install onto an untracked folder of the same `name` (placed by hand or another tool) is treated as a Conflict requiring explicit user confirmation to overwrite/adopt — never a blind overwrite. We chose this conservative invariant over aggressive overwriting because Targets are shared, user-owned directories where silently clobbering hand-placed Skills would be data loss.

## Consequences

- A reasonable reader might expect Apply to "just make the Target match the selection"; this records the deliberate exception so the safety check isn't removed as redundant.
