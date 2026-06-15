package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

func testEngine(t *testing.T, targets ...string) *engine.Engine {
	t.Helper()
	srcRoot := t.TempDir()
	dir := filepath.Join(srcRoot, "deploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: deploy\ndescription: d\n---\nv1"), 0o644)

	var tgt []config.TargetEntry
	for _, name := range targets {
		tgt = append(tgt, config.TargetEntry{Name: name, Path: t.TempDir()})
	}
	cfg := &config.Config{
		Targets: tgt,
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	mp := filepath.Join(t.TempDir(), "m.json")
	return engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, mp)
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestModelRefreshPopulatesSkills(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e).onRefreshed(e.Refresh())
	if len(m.skills) != 1 || m.skills[0].Name != "deploy" {
		t.Fatalf("skills = %+v", m.skills)
	}
	// Not installed yet, so nothing desired.
	if len(selected(m.desired)) != 0 {
		t.Errorf("expected empty desired, got %+v", selected(m.desired))
	}
}

func TestModelSpaceTogglesCell(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e).onRefreshed(e.Refresh())

	updated, _ := m.Update(runes(" "))
	m = updated.(Model)
	if !m.desired[(reconcile.Cell{Skill: "deploy", Source: "local", Target: "cc"})] {
		t.Fatal("space should have selected the current cell")
	}

	updated, _ = m.Update(runes(" "))
	m = updated.(Model)
	if m.desired[(reconcile.Cell{Skill: "deploy", Source: "local", Target: "cc"})] {
		t.Fatal("second space should have deselected the cell")
	}
}

func TestModelAllAndNone(t *testing.T) {
	e := testEngine(t, "t1", "t2")
	m := New(e).onRefreshed(e.Refresh())

	updated, _ := m.Update(runes("a"))
	m = updated.(Model)
	if len(selected(m.desired)) != 2 {
		t.Fatalf("'a' should select both targets, got %+v", selected(m.desired))
	}

	updated, _ = m.Update(runes("n"))
	m = updated.(Model)
	if len(selected(m.desired)) != 0 {
		t.Fatalf("'n' should clear the row, got %+v", selected(m.desired))
	}
}

func TestModelPlanTransition(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e).onRefreshed(e.Refresh())

	updated, _ := m.Update(runes(" ")) // select deploy -> cc
	m = updated.(Model)
	updated, _ = m.Update(runes("p"))
	m = updated.(Model)

	if m.mode != modePlan {
		t.Fatalf("expected modePlan, got %v", m.mode)
	}
	if len(m.plan.Operations) != 1 || m.plan.Operations[0].Kind != reconcile.Install {
		t.Fatalf("expected one install op, got %+v", m.plan.Operations)
	}
}

func TestModelViewDoesNotPanic(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e).onRefreshed(e.Refresh())
	for _, mode := range []viewMode{modeMatrix, modePlan, modeResult} {
		m.mode = mode
		if m.View() == "" {
			t.Errorf("empty view for mode %v", mode)
		}
	}
}
