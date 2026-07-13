package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
)

// envWithUntracked builds a model over a source skill "deploy" (content "v1")
// and a target that already contains an untracked "deploy" folder ("handmade").
func envWithUntracked(t *testing.T) (Model, string /*targetPath*/) {
	t.Helper()
	srcRoot := t.TempDir()
	sdir := filepath.Join(srcRoot, "deploy")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("---\nname: deploy\ndescription: d\n---\nv1"), 0o644)

	targetPath := t.TempDir()
	os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755)
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("handmade"), 0o644)

	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: targetPath}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	e := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))
	return New(e).onRefreshed(e.Refresh()), targetPath
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestPlanConfirmEntersOverwriteWhenCollision(t *testing.T) {
	m, _ := envWithUntracked(t)
	m, _ = step(t, m, runes(" ")) // select deploy -> cc
	m, _ = step(t, m, runes("p"))
	if m.mode != modePlan {
		t.Fatalf("expected modePlan, got %v", m.mode)
	}
	m, cmd := step(t, m, runes("y")) // confirm plan
	if m.mode != modeOverwrite {
		t.Fatalf("collision should route to modeOverwrite, got %v", m.mode)
	}
	if cmd != nil {
		t.Error("no apply command should run before overwrite is confirmed")
	}
	if len(m.preview.Collisions) != 1 || m.preview.Collisions[0].SkillName != "deploy" {
		t.Errorf("collisions = %+v", m.preview.Collisions)
	}
}

func TestPlanPreviewShowsCollisionSection(t *testing.T) {
	m, _ := envWithUntracked(t)
	m, _ = step(t, m, runes(" ")) // select deploy -> cc
	m, _ = step(t, m, runes("p"))
	if m.mode != modePlan {
		t.Fatalf("expected modePlan, got %v", m.mode)
	}
	out := m.viewPlan()
	for _, want := range []string{"will overwrite", "deploy"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan preview missing %q:\n%s", want, out)
		}
	}
}

func TestOverwriteConfirmAdoptsFolder(t *testing.T) {
	m, targetPath := envWithUntracked(t)
	m, _ = step(t, m, runes(" "))
	m, _ = step(t, m, runes("p"))
	m, _ = step(t, m, runes("y")) // -> modeOverwrite

	m, cmd := step(t, m, runes("y")) // confirm overwrite
	if cmd == nil {
		t.Fatal("expected an apply command after confirming overwrite")
	}
	m, _ = step(t, m, cmd()) // run Apply, feed applyDoneMsg

	if m.mode != modeResult {
		t.Fatalf("expected modeResult after apply, got %v", m.mode)
	}
	if !m.report.AllOK() {
		t.Fatalf("apply should have succeeded: %+v", m.report.Results)
	}
	const sourceContent = "---\nname: deploy\ndescription: d\n---\nv1"
	got, _ := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if string(got) != sourceContent {
		t.Errorf("folder not overwritten with source content: %q", got)
	}
}

func TestOverwriteCancelLeavesFolderUntouched(t *testing.T) {
	m, targetPath := envWithUntracked(t)
	m, _ = step(t, m, runes(" "))
	m, _ = step(t, m, runes("p"))
	m, _ = step(t, m, runes("y")) // -> modeOverwrite

	m, cmd := step(t, m, runes("n")) // cancel
	if cmd != nil {
		t.Error("cancel must not trigger apply")
	}
	if m.mode != modeMatrix {
		t.Errorf("cancel should return to matrix, got %v", m.mode)
	}
	got, _ := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if string(got) != "handmade" {
		t.Errorf("untracked folder was modified on cancel: %q", got)
	}
}

func TestPlanConfirmAppliesDirectlyWhenNoCollision(t *testing.T) {
	e := testEngine(t, "cc") // empty target, no untracked folder
	m := New(e).onRefreshed(e.Refresh())
	m, _ = step(t, m, runes(" "))
	m, _ = step(t, m, runes("p"))
	m, cmd := step(t, m, runes("y"))
	if m.mode != modeMatrix {
		t.Fatalf("no collision should apply directly, got mode %v", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected an apply command")
	}
}
