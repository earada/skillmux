// Package domain holds the core vocabulary of Skillmux. See CONTEXT.md for the
// canonical definition of each term.
package domain

import "time"

// SourceKind distinguishes where a Source's Skills come from.
type SourceKind string

const (
	// SourceGitHub is a public GitHub repository fetched as a tarball.
	SourceGitHub SourceKind = "github"
	// SourceLocal is a folder on the local filesystem.
	SourceLocal SourceKind = "local"
)

// Source is an origin that holds one or more Skills. A Source may contain N
// Skills; each directory with a SKILL.md (found recursively) is one Skill.
type Source struct {
	// Name is the user-facing identifier of the Source within the Config.
	Name string
	// Kind is github or local.
	Kind SourceKind
	// Location is the GitHub URL or the local folder path.
	Location string
	// Branch optionally pins a branch/tag (GitHub only). Empty means the
	// repository's default branch.
	Branch string
	// Subpath optionally narrows where Skills are scanned within the Source.
	Subpath string
}

// Target is an AI coding tool on the machine that consumes Skills. In v1 all
// Targets share a homogeneous skill format (a folder containing SKILL.md);
// installing copies the Skill folder into Path.
type Target struct {
	// Name is the user-facing identifier of the Target within the Config.
	Name string
	// Path is the directory Skillmux installs Skills into.
	Path string
}

// Skill is a self-contained directory identified by its SKILL.md. Its identity
// is Name, which is also its install folder name in every Target.
type Skill struct {
	// Name is the skill identity, taken from the SKILL.md frontmatter.
	Name string
	// Description is the human-readable summary from the SKILL.md frontmatter.
	Description string
	// SourceName is the Source this Skill was discovered in.
	SourceName string
	// RelPath is the Skill directory's path relative to its Source root.
	RelPath string
	// Group is the folder hierarchy the Skill sits under within its Source,
	// derived from RelPath (the parent directories, slash-joined). Empty for a
	// Skill at the Source root. Purely organisational — not part of identity.
	Group string
	// Deprecated reports whether the SKILL.md frontmatter marks this Skill as
	// deprecated. DeprecationReason carries the optional human-readable note
	// (e.g. "use new-skill instead") when the field was given as a string.
	Deprecated        bool
	DeprecationReason string
}

// Installation records the fact of a Skill being present in a Target, along
// with the version Fingerprint captured at install time so Skillmux can later
// detect upstream drift (Update available).
type Installation struct {
	// SkillName is the installed Skill's identity.
	SkillName string
	// TargetName is the Target the Skill is installed into.
	TargetName string
	// SourceName is the Source the installed Skill came from.
	SourceName string
	// Path is the Target directory the Skill was installed into at install
	// time (home-expanded, as Apply resolved it). Recorded so a later edit of
	// the Target's Path — keeping its Name — is detected: the files live under
	// this Path, so if the Target now points elsewhere the Installation is
	// stale and must be reinstalled at the new Path. Empty on Installations
	// recorded before this field existed; such entries are grandfathered (a
	// path move cannot be detected for them) rather than treated as moved.
	Path string
	// Fingerprint is the content hash of the Skill folder at install time.
	Fingerprint string
	// InstalledAt is when this Installation was last written.
	InstalledAt time.Time
}

// Revision is the exact point in a GitHub Source's history that its local clone
// currently sits at — a per-Source fact surfaced informationally in the UI
// (every Skill from one clone shares it). Local Sources have no Revision. See
// CONTEXT.md.
type Revision struct {
	// Ref is the branch or tag label the clone tracks, e.g. "main".
	Ref string
	// ShortSHA is the abbreviated commit, e.g. "a1b2c3d".
	ShortSHA string
	// FetchedAt is when the clone was last updated.
	FetchedAt time.Time
}

// Label renders the Revision as "ref @ shortSHA" (e.g. "main @ a1b2c3d"),
// degrading gracefully when either part is missing.
func (r Revision) Label() string {
	switch {
	case r.Ref == "":
		return r.ShortSHA
	case r.ShortSHA == "":
		return r.Ref
	default:
		return r.Ref + " @ " + r.ShortSHA
	}
}

// Status is the state of a (Skill, Target) pair as shown in the TUI.
type Status string

const (
	// StatusNotInstalled means the Skill is not present in the Target.
	StatusNotInstalled Status = "not-installed"
	// StatusUpToDate means the installed Fingerprint matches the Source's
	// current Fingerprint.
	StatusUpToDate Status = "up-to-date"
	// StatusUpdateAvailable means the Source changed since the last install
	// into this Target (upstream drift).
	StatusUpdateAvailable Status = "update-available"
	// StatusConflict means two Skills from different Sources share this Name
	// (and thus install folder) in this Target.
	StatusConflict Status = "conflict"
)
