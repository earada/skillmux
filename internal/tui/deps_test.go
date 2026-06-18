package tui

import (
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

func TestBrokenCellWhenClosureUnsatisfied(t *testing.T) {
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b}}
	m.skills = m.cat.Skills

	cellA := reconcile.Cell{Skill: "a", Source: "local", Target: "cc"}
	cellB := reconcile.Cell{Skill: "b", Source: "local", Target: "cc"}

	// a marked, b absent → a's closure (b) is unsatisfied → a is broken.
	m.desired[cellA] = true
	if !m.brokenCells()[cellA] {
		t.Fatalf("a should be broken when its dependency b is missing from the Target")
	}

	// Mark b too → closure satisfied → nothing broken.
	m.desired[cellB] = true
	if m.brokenCells()[cellA] {
		t.Fatalf("a should not be broken once b is also selected in the Target")
	}
}

func TestBrokenCellIgnoresUnmarkedCell(t *testing.T) {
	// b is referenced but a is not present anywhere — problem-first means a clean
	// matrix: an unmarked, uninstalled cell never reddens.
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b}}
	m.skills = m.cat.Skills

	if len(m.brokenCells()) != 0 {
		t.Fatalf("no cell is present, so none should be broken: %v", m.brokenCells())
	}
}

func TestDepDetailListsNeedsAndSuggests(t *testing.T) {
	// a references b (Dependency) and c; c is reclassified as a Suggestion.
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b", "c"}}
	b := engine.AvailableSkill{Name: "b", Source: "local"}
	c := engine.AvailableSkill{Name: "c", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.eng.Config.Suggestions = []config.SuggestionEntry{{From: "a", To: "c"}}
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b, c}}
	m.skills = m.cat.Skills
	m.row, m.col = 0, 0 // cursor on "a"

	got := m.depDetail()
	for _, want := range []string{"needs:", "b", "suggests:", "c"} {
		if !strings.Contains(got, want) {
			t.Errorf("detail %q missing %q", got, want)
		}
	}
}

func TestDepDetailFlagsCrossSource(t *testing.T) {
	// a (from source "local") needs b, which only "other" offers → cross-Source.
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "other"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b}}
	m.skills = m.cat.Skills
	m.row, m.col = 0, 0

	if got := m.depDetail(); !strings.Contains(got, "(other)") {
		t.Errorf("cross-Source resolution not flagged in %q", got)
	}
}

func TestDepDetailEmptyForLeafSkill(t *testing.T) {
	a := engine.AvailableSkill{Name: "a", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a}}
	m.skills = m.cat.Skills
	m.row, m.col = 0, 0

	if got := m.depDetail(); got != "" {
		t.Errorf("a skill with no edges should have no detail line, got %q", got)
	}
}
