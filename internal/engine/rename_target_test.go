package engine

import (
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// TestUpdateTargetRenameCarriesInstallations exercises skillmux-mdw: renaming a
// Target must re-key its Installations in the Manifest so the renamed Target
// still reports its Skills installed, and re-selecting them plans nothing.
func TestUpdateTargetRenameCarriesInstallations(t *testing.T) {
	e, targetPath, _, manifestPath := newEnv(t)
	cat := e.Refresh()
	if rep, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil || !rep.AllOK() {
		t.Fatalf("Apply: err=%v results=%+v", err, rep.Results)
	}

	if err := e.UpdateTarget("cc", "claude", targetPath); err != nil {
		t.Fatalf("UpdateTarget: %v", err)
	}

	cat = e.Refresh()
	if st := statusOf(e, cat, "deploy", "claude"); st != domain.StatusUpToDate {
		t.Errorf("after rename: got %q, want up-to-date", st)
	}

	pre := e.Preview([]reconcile.Cell{{Skill: "deploy", Source: "local", Target: "claude"}}, cat)
	if len(pre.Plan.Operations) != 0 {
		t.Errorf("expected empty plan after rename, got %+v", pre.Plan.Operations)
	}
	if len(pre.Collisions) != 0 {
		t.Errorf("expected no collisions after rename, got %+v", pre.Collisions)
	}

	persisted, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted.Find("claude", "deploy"); !ok {
		t.Error("persisted manifest lacks the renamed Target's Installation")
	}
	if _, ok := persisted.Find("cc", "deploy"); ok {
		t.Error("persisted manifest still carries the old Target name")
	}
}

// TestUpdateTargetPathOnlyKeepsRecordedPath guards that a path-only edit leaves
// the Manifest untouched, so the existing target-moved Reinstall still fires.
func TestUpdateTargetPathOnlyKeepsRecordedPath(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cat := e.Refresh()
	if _, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil {
		t.Fatal(err)
	}
	before, _ := e.Manifest.Find("cc", "deploy")

	newPath := t.TempDir()
	if err := e.UpdateTarget("cc", "cc", newPath); err != nil {
		t.Fatalf("UpdateTarget: %v", err)
	}
	after, ok := e.Manifest.Find("cc", "deploy")
	if !ok || after != before {
		t.Fatalf("manifest changed by path-only edit: before=%+v after=%+v", before, after)
	}
	pre := e.Preview(cell(), e.Refresh())
	if len(pre.Plan.Operations) != 1 {
		t.Errorf("expected one target-moved reinstall, got %+v", pre.Plan.Operations)
	}
}
