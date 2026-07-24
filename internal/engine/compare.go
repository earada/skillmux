package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/earada/skillmux/internal/diff"
	"github.com/earada/skillmux/internal/fingerprint"
)

// Comparison answers "what would reinstalling this cost me?" for one
// (Skill, Target) pair: the file-level changes between the copy installed in the
// Target and the Skill's current folder in its Source cache, plus how far the
// installed side can be trusted to stand for what upstream looked like at
// install time (see ADR 0007).
type Comparison struct {
	SkillName  string
	SourceName string
	TargetName string
	// InstalledDir is the old side: the Skill's folder inside the Target.
	InstalledDir string
	// SourceDir is the new side: the Skill's folder in the Source cache.
	SourceDir string
	// Tracked reports whether the Manifest records this Installation. An
	// untracked folder is one Apply would need overwrite confirmation to adopt
	// (ADR 0002), and the diff is then "what would be clobbered", not "what
	// changed upstream".
	Tracked bool
	// Pristine reports whether the installed copy still matches the Fingerprint
	// recorded at install time. When false the old side carries hand-made edits
	// too, so the changes below mix local drift with upstream drift.
	Pristine bool
	Summary  diff.Summary
}

// Compare computes the Comparison for sk against the named Target. It errors
// when the Target is unknown, when the Skill has no folder to compare against
// (removed upstream, or its Source has not been downloaded), or when the Target
// holds no copy of the Skill.
//
// It takes the operation lock: Refresh rewrites a Source's working tree and
// Apply rewrites a Target's folder, so reading both sides while either runs
// could produce a diff of a half-written tree. Callers run it off the UI loop.
func (e *Engine) Compare(sk AvailableSkill, targetName string) (Comparison, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()

	targetPath, ok := e.targetPaths()[targetName]
	if !ok {
		return Comparison{}, fmt.Errorf("unknown target %q", targetName)
	}
	if sk.Dir == "" {
		return Comparison{}, fmt.Errorf("%s is not available from source %q — nothing to compare against", sk.Name, sk.Source)
	}
	installedDir := filepath.Join(targetPath, sk.Name)
	if info, err := os.Stat(installedDir); err != nil || !info.IsDir() {
		return Comparison{}, fmt.Errorf("%s is not installed in %s — nothing to compare", sk.Name, targetName)
	}

	c := Comparison{
		SkillName:    sk.Name,
		SourceName:   sk.Source,
		TargetName:   targetName,
		InstalledDir: installedDir,
		SourceDir:    sk.Dir,
	}
	if in, tracked := e.Manifest.Find(targetName, sk.Name); tracked {
		c.Tracked = true
		fp, err := fingerprint.Dir(installedDir)
		c.Pristine = err == nil && fp == in.Fingerprint
	}

	sum, err := diff.Compare(installedDir, sk.Dir)
	if err != nil {
		return Comparison{}, err
	}
	c.Summary = sum
	return c, nil
}

// InstalledCopy returns the folder a Skill occupies in a Target and whether a
// copy is actually there. Callers use it to decide, synchronously and cheaply,
// whether a Compare is worth dispatching at all.
func (e *Engine) InstalledCopy(targetName, skillName string) (string, bool) {
	targetPath, ok := e.targetPaths()[targetName]
	if !ok {
		return "", false
	}
	dir := filepath.Join(targetPath, skillName)
	info, err := os.Stat(dir)
	return dir, err == nil && info.IsDir()
}
