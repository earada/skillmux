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

func TestMarkClosureMarksSkillAndTransitiveDeps(t *testing.T) {
	// a → b → c: pressing 'd' on a should mark a, b and c in the cursor Target,
	// curing the amber in one keystroke.
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "local", Refs: []string{"c"}}
	c := engine.AvailableSkill{Name: "c", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b, c}}
	m.skills = m.cat.Skills
	m.targets = m.eng.Config.DomainTargets()
	m.row, m.col = 0, 0 // cursor on "a", target "cc"

	m.markClosure()

	for _, name := range []string{"a", "b", "c"} {
		cell := reconcile.Cell{Skill: name, Source: "local", Target: "cc"}
		if !m.desired[cell] {
			t.Errorf("expected %s to be marked in cc after pressing d", name)
		}
	}
	// And the cell is no longer broken.
	if m.brokenCells()[reconcile.Cell{Skill: "a", Source: "local", Target: "cc"}] {
		t.Errorf("a should be satisfied after its closure is marked")
	}
}

func TestMatrixDKeyMarksClosure(t *testing.T) {
	// End-to-end through Update: the 'd' keypress mutates the shared desired map
	// despite Update's value receiver.
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b}}
	m.skills = m.cat.Skills
	m.row, m.col = 0, 0

	updated, _ := m.Update(runes("d"))
	m = updated.(Model)

	if !m.desired[reconcile.Cell{Skill: "b", Source: "local", Target: "cc"}] {
		t.Errorf("pressing d should have marked dependency b")
	}
}

func TestMarkClosureStaysConflictFree(t *testing.T) {
	// b is offered by two Sources; marking a's closure must pick one and clear
	// the rival so the selection never holds two Sources for the same name.
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b1 := engine.AvailableSkill{Name: "b", Source: "local"}
	b2 := engine.AvailableSkill{Name: "b", Source: "other"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b1, b2}}
	m.skills = m.cat.Skills
	m.targets = m.eng.Config.DomainTargets()
	m.row, m.col = 0, 0

	m.markClosure()

	local := m.desired[reconcile.Cell{Skill: "b", Source: "local", Target: "cc"}]
	other := m.desired[reconcile.Cell{Skill: "b", Source: "other", Target: "cc"}]
	if local == other {
		t.Errorf("exactly one Source of b should be marked, got local=%v other=%v", local, other)
	}
	// Resolution prefers a's own Source (local).
	if !local {
		t.Errorf("closure should prefer the depending Skill's own Source for b")
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
