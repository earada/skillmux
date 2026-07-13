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
	return engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", mp)
}

// testEngineSkills builds an engine whose single source offers the named skills.
func testEngineSkills(t *testing.T, names ...string) *engine.Engine {
	t.Helper()
	srcRoot := t.TempDir()
	for _, n := range names {
		dir := filepath.Join(srcRoot, n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "SKILL.md"),
			[]byte("---\nname: "+n+"\ndescription: d\n---\nv1"), 0o644)
	}
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	mp := filepath.Join(t.TempDir(), "m.json")
	return engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", mp)
}

// testEngineCfg is testEngineSkills with a real config path, so Config
// mutations (e.g. toggling a Suggestion in the 'v' view) can persist.
func testEngineCfg(t *testing.T) *engine.Engine {
	t.Helper()
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
		Sources: []config.SourceEntry{{Name: "local", Location: t.TempDir()}},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	mp := filepath.Join(t.TempDir(), "m.json")
	return engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, configPath, mp)
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestModelFilterNarrowsRows(t *testing.T) {
	e := testEngineSkills(t, "deploy", "build", "rebuild")
	m := New(e).onRefreshed(e.Refresh())
	if got := len(m.rows()); got != 3 {
		t.Fatalf("expected 3 skills, got %d", got)
	}

	// "/" opens the search line; typing "build" filters live.
	updated, _ := m.Update(runes("/"))
	m = updated.(Model)
	if !m.searching {
		t.Fatal("'/' should start searching")
	}
	for _, r := range "build" {
		updated, _ = m.Update(runes(string(r)))
		m = updated.(Model)
	}
	rows := m.rows()
	if len(rows) != 2 {
		t.Fatalf("expected build+rebuild, got %+v", rows)
	}
	for _, s := range rows {
		if s.Name != "build" && s.Name != "rebuild" {
			t.Errorf("unexpected match %q", s.Name)
		}
	}
}

func TestModelFilterEnterKeepsAndEscClears(t *testing.T) {
	e := testEngineSkills(t, "deploy", "build")
	m := New(e).onRefreshed(e.Refresh())

	m = applyKeys(m, runes("/"), runes("d"), runes("e"), runes("p"))
	enter, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = enter.(Model)
	if m.searching {
		t.Fatal("enter should leave the search line")
	}
	if m.filter != "dep" || len(m.rows()) != 1 {
		t.Fatalf("enter should keep filter; filter=%q rows=%d", m.filter, len(m.rows()))
	}

	esc, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = esc.(Model)
	if m.filter != "" || len(m.rows()) != 2 {
		t.Fatalf("esc should clear filter; filter=%q rows=%d", m.filter, len(m.rows()))
	}
}

func TestModelFilterCursorStaysInRange(t *testing.T) {
	e := testEngineSkills(t, "deploy", "build", "rebuild")
	m := New(e).onRefreshed(e.Refresh())
	m.row = 2 // last row of the unfiltered list

	// Filter down to a single match: the cursor must snap back into range.
	m = applyKeys(m, runes("/"), runes("d"), runes("e"), runes("p"))
	if m.row >= len(m.rows()) {
		t.Fatalf("cursor row %d out of range for %d rows", m.row, len(m.rows()))
	}
	if _, ok := m.curSkill(); !ok {
		t.Fatal("curSkill should resolve after filtering")
	}
}

func applyKeys(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		u, _ := m.Update(k)
		m = u.(Model)
	}
	return m
}

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
	if len(m.preview.Plan.Operations) != 1 || m.preview.Plan.Operations[0].Kind != reconcile.Install {
		t.Fatalf("expected one install op, got %+v", m.preview.Plan.Operations)
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
