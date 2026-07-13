package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

func installOp(skill, target string) reconcile.Operation {
	return reconcile.Operation{Kind: reconcile.Install, SkillName: skill, SourceName: "local", TargetName: target}
}

func TestCollisionsDetectsUntrackedFolder(t *testing.T) {
	targetPath := t.TempDir()
	// An untracked folder placed by hand where "deploy" would install.
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	targets := map[string]string{"cc": targetPath}
	plan := reconcile.Plan{Operations: []reconcile.Operation{installOp("deploy", "cc")}}

	cols := Collisions(plan, targets, &manifest.Manifest{})
	if len(cols) != 1 {
		t.Fatalf("expected 1 collision, got %+v", cols)
	}
	if cols[0].SkillName != "deploy" || cols[0].TargetName != "cc" || cols[0].Dir == "" {
		t.Errorf("collision metadata wrong: %+v", cols[0])
	}
}

func TestCollisionsEmptyWhenDestFree(t *testing.T) {
	targets := map[string]string{"cc": t.TempDir()} // nothing at the destination
	plan := reconcile.Plan{Operations: []reconcile.Operation{installOp("deploy", "cc")}}
	if cols := Collisions(plan, targets, &manifest.Manifest{}); len(cols) != 0 {
		t.Fatalf("expected no collisions, got %+v", cols)
	}
}

func TestCollisionsEmptyWhenTracked(t *testing.T) {
	targetPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The folder exists but the Manifest tracks it — Skillmux placed it, so it is
	// not a collision.
	man := &manifest.Manifest{}
	man.Put(domain.Installation{SkillName: "deploy", TargetName: "cc", SourceName: "local"})
	targets := map[string]string{"cc": targetPath}
	plan := reconcile.Plan{Operations: []reconcile.Operation{installOp("deploy", "cc")}}
	if cols := Collisions(plan, targets, man); len(cols) != 0 {
		t.Fatalf("a tracked folder is not a collision, got %+v", cols)
	}
}

func TestCollisionsIgnoresTrackedReinstall(t *testing.T) {
	targetPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A Reinstall is only ever emitted when the Skill is already tracked, so the
	// same untracked-overwrite predicate returns false for it — no special-casing
	// of Install vs Reinstall, and the pre-flight agrees with what install would
	// do at write time.
	man := &manifest.Manifest{}
	man.Put(domain.Installation{SkillName: "deploy", TargetName: "cc", SourceName: "local", Fingerprint: "old"})
	targets := map[string]string{"cc": targetPath}
	plan := reconcile.Plan{Operations: []reconcile.Operation{
		{Kind: reconcile.Reinstall, SkillName: "deploy", SourceName: "local", TargetName: "cc", Reason: reconcile.ReasonUpdateAvailable},
	}}
	if cols := Collisions(plan, targets, man); len(cols) != 0 {
		t.Fatalf("reinstall of a tracked skill should not be a collision, got %+v", cols)
	}
}
