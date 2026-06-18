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

func TestBrokenListReportsMissingClosure(t *testing.T) {
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b}}
	m.skills = m.cat.Skills
	m.desired[reconcile.Cell{Skill: "a", Source: "local", Target: "cc"}] = true

	broken := m.brokenList()
	if len(broken) != 1 {
		t.Fatalf("expected 1 broken entry, got %d: %+v", len(broken), broken)
	}
	if broken[0].Cell.Skill != "a" || len(broken[0].Missing) != 1 || broken[0].Missing[0].Name != "b" {
		t.Fatalf("expected a needs b, got %+v", broken[0])
	}
	if !fixable(broken) {
		t.Errorf("a missing b (offered by local) should be fixable")
	}
}

func TestViewPlanRendersBrokenSection(t *testing.T) {
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b}}
	m.skills = m.cat.Skills
	m.desired[reconcile.Cell{Skill: "a", Source: "local", Target: "cc"}] = true
	m.mode = modePlan

	out := m.viewPlan()
	for _, want := range []string{"broken", "needs", "a", "b"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan view missing %q:\n%s", want, out)
		}
	}
}

func TestFixBrokenAddsMissingClosureToDesired(t *testing.T) {
	// Plan 'f': a needs b, only a is marked → after fixBroken b is marked too and
	// nothing is broken.
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "local"}
	m := New(testEngineSkills(t, "x"))
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{a, b}}
	m.skills = m.cat.Skills
	m.desired[reconcile.Cell{Skill: "a", Source: "local", Target: "cc"}] = true

	m.fixBroken()

	if !m.desired[reconcile.Cell{Skill: "b", Source: "local", Target: "cc"}] {
		t.Errorf("f should have marked the missing dependency b")
	}
	if len(m.brokenList()) != 0 {
		t.Errorf("nothing should be broken after fix: %+v", m.brokenList())
	}
}

func TestPlanFKeyFixesAndRecomputes(t *testing.T) {
	// End-to-end: open the Plan with a broken selection, press 'f', and the new
	// dependency Install appears in the recomputed plan.
	e := testEngine(t, "cc") // source "local" offers skill "deploy"
	m := New(e).onRefreshed(e.Refresh())
	// Make deploy depend on a second skill that isn't selected.
	dep := engine.AvailableSkill{Name: "deploy", Source: "local", Refs: []string{"helper"}}
	helper := engine.AvailableSkill{Name: "helper", Source: "local"}
	m.cat = engine.Catalog{SourceErrors: map[string]error{}, Skills: []engine.AvailableSkill{dep, helper}}
	m.skills = m.cat.Skills
	m.desired = map[reconcile.Cell]bool{{Skill: "deploy", Source: "local", Target: "cc"}: true}

	m.plan = m.eng.Plan(selected(m.desired), m.cat)
	m.mode = modePlan
	if len(m.brokenList()) != 1 {
		t.Fatalf("expected a broken entry before fixing, got %+v", m.brokenList())
	}

	updated, _ := m.Update(runes("f"))
	m = updated.(Model)

	if m.mode != modePlan {
		t.Fatalf("f should stay in the Plan, got %v", m.mode)
	}
	if len(m.brokenList()) != 0 {
		t.Errorf("plan should no longer be broken after f: %+v", m.brokenList())
	}
	var installsHelper bool
	for _, op := range m.plan.Operations {
		if op.SkillName == "helper" && op.Kind == reconcile.Install {
			installsHelper = true
		}
	}
	if !installsHelper {
		t.Errorf("recomputed plan should install the missing dependency helper: %+v", m.plan.Operations)
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
