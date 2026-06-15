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
	// Fingerprint is the content hash of the Skill folder at install time.
	Fingerprint string
	// InstalledAt is when this Installation was last written.
	InstalledAt time.Time
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
