package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"

	"github.com/earada/skillmux/internal/apply"
)

// envWithModified installs "deploy" into a target, then hand-edits the
// installed copy AND drifts the source, so a reinstall is planned that would
// clobber local edits.
func envWithModified(t *testing.T) (Model, string /*targetPath*/) {
	t.Helper()
	srcRoot := t.TempDir()
	sdir := filepath.Join(srcRoot, "deploy")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("---\nname: deploy\ndescription: d\n---\nv1"), 0o644)

	targetPath := t.TempDir()
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: targetPath}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	e := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))

	// Install pristinely, then hand-edit the copy and drift the source.
	cat := e.Refresh()
	pre := e.Preview([]reconcile.Cell{{Skill: "deploy", Source: "local", Target: "cc"}}, cat)
	if rep, err := e.Apply(pre, apply.Options{}); err != nil || !rep.AllOK() {
		t.Fatalf("install failed: err=%v rep=%+v", err, rep)
	}
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("my local tweaks"), 0o644)
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("---\nname: deploy\ndescription: d\n---\nv2"), 0o644)

	return New(e).onRefreshed(e.Refresh()), targetPath
}

func TestMatrixShowsModifiedStatus(t *testing.T) {
	m, _ := envWithModified(t)
	st := m.status[statusKey{"deploy", "local", "cc"}]
	if st != "modified-locally" {
		t.Fatalf("cell status = %v, want modified-locally", st)
	}
	// A modified skill is still installed: its cell starts desired, so the plan
	// does not uninstall it behind the user's back.
	if !m.desired[reconcile.Cell{Skill: "deploy", Source: "local", Target: "cc"}] {
		t.Error("modified installation should start desired")
	}
}

func TestPlanShowsModifiedSectionAndRoutesToOverwrite(t *testing.T) {
	m, _ := envWithModified(t)
	m, _ = step(t, m, runes("p"))
	if m.mode != modePlan {
		t.Fatalf("expected modePlan, got %v", m.mode)
	}
	if out := m.viewPlan(); !strings.Contains(out, "modified locally") {
		t.Errorf("plan preview missing modified section:\n%s", out)
	}
	m, cmd := step(t, m, runes("y"))
	if m.mode != modeOverwrite {
		t.Fatalf("modified overwrite should route to modeOverwrite, got %v", m.mode)
	}
	if cmd != nil {
		t.Error("no apply command should run before the overwrite is confirmed")
	}
	if out := m.viewOverwrite(); !strings.Contains(out, "DISCARDS") {
		t.Errorf("overwrite screen should warn about discarding edits:\n%s", out)
	}
}

func TestOverwriteConfirmDiscardsLocalEdits(t *testing.T) {
	m, targetPath := envWithModified(t)
	m, _ = step(t, m, runes("p"))
	m, _ = step(t, m, runes("y")) // -> modeOverwrite
	m, cmd := step(t, m, runes("y"))
	if cmd == nil {
		t.Fatal("expected an apply command after confirming")
	}
	m, _ = step(t, m, cmd())
	if !m.report.AllOK() {
		t.Fatalf("apply should have succeeded: %+v", m.report.Results)
	}
	got, _ := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if !strings.Contains(string(got), "v2") {
		t.Errorf("confirmed reinstall should land v2, got %q", got)
	}
}

func TestOverwriteCancelKeepsLocalEdits(t *testing.T) {
	m, targetPath := envWithModified(t)
	m, _ = step(t, m, runes("p"))
	m, _ = step(t, m, runes("y")) // -> modeOverwrite
	m, cmd := step(t, m, runes("n"))
	if cmd != nil {
		t.Error("cancel must not trigger apply")
	}
	got, _ := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if string(got) != "my local tweaks" {
		t.Errorf("cancel must keep the local edits, got %q", got)
	}
}
