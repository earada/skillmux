package domain

// IsVCSMetadata reports whether a directory entry named name is version-control
// metadata rather than Skill content: a `.git` directory, or the `.git` gitfile
// a worktree or submodule uses. It applies at any depth. Every walker over a
// Skill folder or Source (scan, fingerprint, copy, diff, the viewer) excludes
// such entries so they all agree on what a Skill is. See skillmux-ia0.
func IsVCSMetadata(name string) bool {
	return name == ".git"
}
