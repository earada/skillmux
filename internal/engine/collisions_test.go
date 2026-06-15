package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

func TestCollisionsDetectsUntrackedFolder(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	// An untracked folder placed by hand where "deploy" would install.
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	cat := e.Refresh()
	plan := e.Plan(cell(), cat)

	cols := e.Collisions(plan)
	if len(cols) != 1 {
		t.Fatalf("expected 1 collision, got %+v", cols)
	}
	if cols[0].SkillName != "deploy" || cols[0].TargetName != "cc" || cols[0].Dir == "" {
		t.Errorf("collision metadata wrong: %+v", cols[0])
	}
}

func TestCollisionsEmptyWhenDestFree(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cat := e.Refresh()
	plan := e.Plan(cell(), cat)
	if cols := e.Collisions(plan); len(cols) != 0 {
		t.Fatalf("expected no collisions, got %+v", cols)
	}
}

func TestCollisionsIgnoresTrackedReinstall(t *testing.T) {
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
	}
	man := &manifest.Manifest{}
	man.Put(installation("deploy", "cc", "local", "old"))
	e := New(cfg, man, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))

	// A reinstall op (tracked) must never be treated as a collision.
	plan := reconcile.Plan{Operations: []reconcile.Operation{
		{Kind: reconcile.Reinstall, SkillName: "deploy", SourceName: "local", TargetName: "cc", Reason: reconcile.ReasonUpdateAvailable},
	}}
	if cols := e.Collisions(plan); len(cols) != 0 {
		t.Fatalf("reinstall should not be a collision, got %+v", cols)
	}
}

func installation(skill, target, source, fp string) domain.Installation {
	return domain.Installation{SkillName: skill, TargetName: target, SourceName: source, Fingerprint: fp}
}
