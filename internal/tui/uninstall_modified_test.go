package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/reconcile"
)

// envWithModifiedUninstall installs "deploy", hand-edits the installed copy,
// and deselects the cell so the Plan would uninstall it (skillmux-9nn).
func envWithModifiedUninstall(t *testing.T) (Model, string /*targetPath*/) {
	t.Helper()
	m, targetPath := envWithModified(t)
	m.desired[reconcile.Cell{Skill: "deploy", Source: "local", Target: "cc"}] = false
	return m, targetPath
}

func TestPlanWarnsAndConfirmsModifiedUninstall(t *testing.T) {
	m, targetPath := envWithModifiedUninstall(t)
	m, _ = step(t, m, runes("p"))
	if m.mode != modePlan {
		t.Fatalf("expected modePlan, got %v", m.mode)
	}
	if ops := m.preview.Plan.Operations; len(ops) != 1 || ops[0].Kind != reconcile.Uninstall {
		t.Fatalf("expected one uninstall, got %+v", ops)
	}
	if out := m.viewPlan(); !strings.Contains(out, "modified locally") {
		t.Errorf("plan preview missing modified section:\n%s", out)
	}
	m, cmd := step(t, m, runes("y"))
	if m.mode != modeOverwrite {
		t.Fatalf("modified uninstall should route to confirmation, got %v", m.mode)
	}
	if cmd != nil {
		t.Error("no apply command should run before the uninstall is confirmed")
	}
	if out := m.viewOverwrite(); !strings.Contains(out, "DISCARDS") {
		t.Errorf("confirmation screen should warn about discarding edits:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); err != nil {
		t.Fatalf("copy must survive until confirmed: %v", err)
	}

	m, cmd = step(t, m, runes("y"))
	if cmd == nil {
		t.Fatal("expected an apply command after confirming")
	}
	m, _ = step(t, m, cmd())
	if !m.report.AllOK() {
		t.Fatalf("confirmed uninstall should succeed: %+v", m.report.Results)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Errorf("confirmed uninstall should remove the copy, stat err=%v", err)
	}
}

func TestModifiedUninstallCancelKeepsLocalEdits(t *testing.T) {
	m, targetPath := envWithModifiedUninstall(t)
	m, _ = step(t, m, runes("p"))
	m, _ = step(t, m, runes("y")) // -> confirmation
	m, cmd := step(t, m, runes("n"))
	if cmd != nil {
		t.Error("cancel must not trigger apply")
	}
	got, _ := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if string(got) != "my local tweaks" {
		t.Errorf("cancel must keep the local edits, got %q", got)
	}
}
