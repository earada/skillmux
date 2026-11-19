package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/manifest"
)

// installDeployFq2 installs "deploy" into "cc" through the real Preview/Apply path.
func installDeployFq2(t *testing.T, e *Engine) Catalog {
	t.Helper()
	cat := e.Refresh()
	rep, err := e.Apply(e.Preview(cell(), cat), apply.Options{})
	if err != nil || !rep.AllOK() {
		t.Fatalf("install failed: err=%v report=%+v", err, rep)
	}
	return cat
}

// Removing a Target forgets its Installations (in memory and on disk) but leaves
// the installed files alone (skillmux-fq2).
func TestRemoveTargetPrunesManifest(t *testing.T) {
	e, targetPath, _, manifestPath := newEnv(t)
	installDeployFq2(t, e)

	if err := e.RemoveTarget("cc"); err != nil {
		t.Fatalf("RemoveTarget: %v", err)
	}
	if n := len(e.Manifest.Installations); n != 0 {
		t.Fatalf("in-memory manifest should be empty, got %+v", e.Manifest.Installations)
	}
	onDisk, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(onDisk.Installations); n != 0 {
		t.Fatalf("persisted manifest should be empty, got %+v", onDisk.Installations)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy", "SKILL.md")); err != nil {
		t.Fatalf("installed files must stay on disk: %v", err)
	}

	cat := e.Refresh()
	if ops := e.Preview(nil, cat).Plan.Operations; len(ops) != 0 {
		t.Fatalf("plan should be empty after removing the Target, got %+v", ops)
	}
}

// A Target removed from the Config by hand (Manifest untouched) must never
// produce a Plan operation or a Status row, and its Manifest entries survive.
func TestHandRemovedTargetIsInert(t *testing.T) {
	e, _, skillDir, _ := newEnv(t)
	installDeployFq2(t, e)
	e.Config.Targets = nil

	cat := e.Refresh()
	if ops := e.Preview(nil, cat).Plan.Operations; len(ops) != 0 {
		t.Fatalf("plan should be empty, got %+v", ops)
	}
	if rows := e.Status(cat); len(rows) != 0 {
		t.Fatalf("status should have no rows, got %+v", rows)
	}

	// Also when the Skill vanished upstream (the StatusUnavailable path).
	if err := os.RemoveAll(skillDir); err != nil {
		t.Fatal(err)
	}
	cat = e.Refresh()
	if rows := e.Status(cat); len(rows) != 0 {
		t.Fatalf("status should have no unavailable rows, got %+v", rows)
	}
	if ops := e.Preview(nil, cat).Plan.Operations; len(ops) != 0 {
		t.Fatalf("plan should be empty, got %+v", ops)
	}
	if n := len(e.Manifest.Installations); n != 1 {
		t.Fatalf("manifest entries must be left untouched, got %+v", e.Manifest.Installations)
	}
}

// A Skill installed only in a Target removed from the Config by hand, and since
// dropped upstream, must not surface as an empty "unavailable" row.
func TestUnavailableSkillsIgnoresUnconfiguredTarget(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cat := e.Refresh()
	if rep, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil || !rep.AllOK() {
		t.Fatalf("install failed: %v %+v", err, rep)
	}
	e.Config.Targets = nil
	if got := e.UnavailableSkills(Catalog{}); len(got) != 0 {
		t.Fatalf("expected no unavailable rows for an unconfigured Target, got %+v", got)
	}
}
