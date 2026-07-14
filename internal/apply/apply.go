// Package apply executes a reconcile.Plan against the filesystem: it copies
// Skill folders into Targets, removes uninstalled ones, and updates the
// Manifest. It runs best-effort (one failed operation does not stop the rest)
// and upholds the invariant that Skillmux only mutates what it manages — it
// never clobbers an untracked folder without explicit confirmation. See
// ADR 0002.
package apply

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// SkillID identifies an available Skill by its Source and Name.
type SkillID struct {
	Source string
	Skill  string
}

// ResolvedSkill is where an available Skill's folder lives on disk (in the
// Source cache) and the fingerprint to record once it is installed.
type ResolvedSkill struct {
	Dir         string
	Fingerprint string
}

// Options tunes an Apply run.
type Options struct {
	// ConfirmOverwrite is consulted when an Install or Reinstall would write
	// over a folder that exists at the Target but is not tracked in the
	// Manifest. Returning false — the default behaviour when nil — refuses the
	// write, leaving the untracked folder untouched.
	ConfirmOverwrite func(targetName, skillName, destDir string) bool
	// now overrides the install timestamp; defaults to time.Now.
	now func() time.Time
}

// Result is the outcome of one operation.
type Result struct {
	Op  reconcile.Operation
	OK  bool
	Err error
}

// Report is the per-operation outcome of an Apply run.
type Report struct {
	Results []Result
}

// AllOK reports whether every operation succeeded.
func (r Report) AllOK() bool {
	for _, res := range r.Results {
		if !res.OK {
			return false
		}
	}
	return true
}

// Collision is an Install or Reinstall whose destination folder already exists
// at the Target but is not tracked in the Manifest — placed by hand or another
// tool. Skillmux refuses to overwrite it without explicit confirmation (ADR
// 0002). engine.Preview surfaces these; install enforces the refusal.
type Collision struct {
	SkillName  string
	SourceName string
	TargetName string
	Dir        string
}

// Collisions reports every operation in plan whose destination is an untracked
// folder that would be clobbered. It is the pre-flight projection of the very
// collides predicate install enforces at write time, so a preview and the
// enforcement can never disagree about what counts as a collision.
func Collisions(plan reconcile.Plan, targets map[string]string, man *manifest.Manifest) []Collision {
	var out []Collision
	for _, op := range plan.Operations {
		if c, ok := collides(op, targets, man); ok {
			out = append(out, c)
		}
	}
	return out
}

// collides is the single definition of the untracked-overwrite invariant: it
// reports whether op would write over a folder that exists at the Target but is
// not recorded in the Manifest. Consulted by both Collisions (pre-flight) and
// install (write-time), so the two can never drift. A Reinstall is only ever
// emitted when the Skill is already tracked, so it never collides — the
// predicate gives the right answer per op without special-casing kind.
func collides(op reconcile.Operation, targets map[string]string, man *manifest.Manifest) (Collision, bool) {
	switch op.Kind {
	case reconcile.Install, reconcile.Reinstall:
	default:
		return Collision{}, false
	}
	targetPath, ok := targets[op.TargetName]
	if !ok {
		return Collision{}, false
	}
	if _, tracked := man.Find(op.TargetName, op.SkillName); tracked {
		return Collision{}, false
	}
	dir := filepath.Join(targetPath, op.SkillName)
	if !exists(dir) {
		return Collision{}, false
	}
	return Collision{
		SkillName:  op.SkillName,
		SourceName: op.SourceName,
		TargetName: op.TargetName,
		Dir:        dir,
	}, true
}

// Apply carries out plan, mutating man in memory but NOT persisting it — the
// caller (engine.Apply) owns persistence. It is the internal disk executor of
// the Preview→Apply seam; callers outside the engine (its own tests) must
// remember to persist the Manifest themselves. targets maps Target name to its
// path; resolved maps an available Skill to its cached folder and fingerprint.
func Apply(plan reconcile.Plan, targets map[string]string, resolved map[SkillID]ResolvedSkill, man *manifest.Manifest, opts Options) Report {
	now := opts.now
	if now == nil {
		now = time.Now
	}

	var rep Report
	for _, op := range plan.Operations {
		err := applyOne(op, targets, resolved, man, opts, now)
		rep.Results = append(rep.Results, Result{Op: op, OK: err == nil, Err: err})
	}
	return rep
}

func applyOne(op reconcile.Operation, targets map[string]string, resolved map[SkillID]ResolvedSkill, man *manifest.Manifest, opts Options, now func() time.Time) error {
	switch op.Kind {
	case reconcile.Install, reconcile.Reinstall:
		return install(op, targets, resolved, man, opts, now)
	case reconcile.Uninstall:
		return uninstall(op, targets, man)
	case reconcile.Conflict:
		return fmt.Errorf("unresolved conflict for skill %q in target %q", op.SkillName, op.TargetName)
	default:
		return fmt.Errorf("unknown operation kind %q", op.Kind)
	}
}

func install(op reconcile.Operation, targets map[string]string, resolved map[SkillID]ResolvedSkill, man *manifest.Manifest, opts Options, now func() time.Time) error {
	targetPath, ok := targets[op.TargetName]
	if !ok {
		return fmt.Errorf("unknown target %q", op.TargetName)
	}
	rs, ok := resolved[SkillID{Source: op.SourceName, Skill: op.SkillName}]
	if !ok {
		return fmt.Errorf("skill %q not available from source %q", op.SkillName, op.SourceName)
	}

	dest, err := destWithin(targetPath, op.SkillName)
	if err != nil {
		return err
	}
	if _, collision := collides(op, targets, man); collision {
		// Safety invariant (ADR 0002): never clobber a folder we did not install
		// without explicit confirmation. Same predicate the pre-flight uses.
		if opts.ConfirmOverwrite == nil || !opts.ConfirmOverwrite(op.TargetName, op.SkillName, dest) {
			return fmt.Errorf("refusing to overwrite untracked folder %s (confirm to adopt)", dest)
		}
	}

	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("clearing destination: %w", err)
	}
	if err := copyDir(rs.Dir, dest); err != nil {
		return fmt.Errorf("copying skill: %w", err)
	}
	man.Put(domain.Installation{
		SkillName:   op.SkillName,
		TargetName:  op.TargetName,
		SourceName:  op.SourceName,
		Fingerprint: rs.Fingerprint,
		InstalledAt: now().UTC(),
	})
	return nil
}

func uninstall(op reconcile.Operation, targets map[string]string, man *manifest.Manifest) error {
	targetPath, ok := targets[op.TargetName]
	if !ok {
		return fmt.Errorf("unknown target %q", op.TargetName)
	}
	dest, err := destWithin(targetPath, op.SkillName)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("removing skill: %w", err)
	}
	man.Remove(op.TargetName, op.SkillName)
	return nil
}

// destWithin joins skillName onto targetPath and verifies the result is a
// proper subpath of targetPath. It is the write-time backstop to the scanner's
// name validation (see skillmux-aps): even if a malformed name reaches Apply,
// no RemoveAll or copy may touch a path at or outside the configured Target. A
// name resolving to the Target itself is rejected too, so a stray "." can never
// clear the whole Target.
func destWithin(targetPath, skillName string) (string, error) {
	dest := filepath.Join(targetPath, skillName)
	rel, err := filepath.Rel(targetPath, dest)
	if err != nil {
		return "", fmt.Errorf("skill %q: resolving destination: %w", skillName, err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("skill %q resolves outside target %s", skillName, targetPath)
	}
	return dest, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// copyDir recursively copies the regular files and directory structure of src
// into dst, recreating dst. Symlinks and other special files are skipped, to
// match the fingerprint's notion of Skill content.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
