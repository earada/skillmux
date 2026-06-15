package tui

import (
	"testing"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

func TestInitialDesiredMarksInstalledCells(t *testing.T) {
	cells := []engine.CellStatus{
		{SkillName: "a", SourceName: "s", TargetName: "t1", Status: domain.StatusUpToDate},
		{SkillName: "a", SourceName: "s", TargetName: "t2", Status: domain.StatusUpdateAvailable},
		{SkillName: "b", SourceName: "s", TargetName: "t1", Status: domain.StatusNotInstalled},
	}
	d := initialDesired(cells)
	if !d[reconcile.Cell{Skill: "a", Source: "s", Target: "t1"}] {
		t.Error("up-to-date cell should be desired")
	}
	if !d[reconcile.Cell{Skill: "a", Source: "s", Target: "t2"}] {
		t.Error("update-available cell should be desired")
	}
	if d[reconcile.Cell{Skill: "b", Source: "s", Target: "t1"}] {
		t.Error("not-installed cell should not be desired")
	}
}

func TestSelectedReturnsSortedTrueCells(t *testing.T) {
	d := map[reconcile.Cell]bool{
		{Skill: "b", Source: "s", Target: "t1"}: true,
		{Skill: "a", Source: "s", Target: "t1"}: true,
		{Skill: "c", Source: "s", Target: "t1"}: false,
	}
	got := selected(d)
	if len(got) != 2 {
		t.Fatalf("expected 2 selected, got %d", len(got))
	}
	if got[0].Skill != "a" || got[1].Skill != "b" {
		t.Errorf("not sorted deterministically: %+v", got)
	}
}

func TestSetRowAllAndNone(t *testing.T) {
	targets := []domain.Target{{Name: "t1"}, {Name: "t2"}}
	skills := []engine.AvailableSkill{{Name: "deploy", Source: "s"}}
	d := map[reconcile.Cell]bool{}
	setRow(d, "deploy", "s", targets, skills, true)
	if !d[reconcile.Cell{Skill: "deploy", Source: "s", Target: "t1"}] ||
		!d[reconcile.Cell{Skill: "deploy", Source: "s", Target: "t2"}] {
		t.Error("setRow(true) should select all targets for the row")
	}
	setRow(d, "deploy", "s", targets, skills, false)
	if d[reconcile.Cell{Skill: "deploy", Source: "s", Target: "t1"}] ||
		d[reconcile.Cell{Skill: "deploy", Source: "s", Target: "t2"}] {
		t.Error("setRow(false) should clear all targets for the row")
	}
}

func TestSelectCellIsExclusivePerNameAndTarget(t *testing.T) {
	skills := []engine.AvailableSkill{
		{Name: "deploy", Source: "a"},
		{Name: "deploy", Source: "b"},
	}
	d := map[reconcile.Cell]bool{}
	selectCell(d, reconcile.Cell{Skill: "deploy", Source: "a", Target: "t1"}, skills)
	selectCell(d, reconcile.Cell{Skill: "deploy", Source: "b", Target: "t1"}, skills)

	if d[reconcile.Cell{Skill: "deploy", Source: "a", Target: "t1"}] {
		t.Error("selecting source b should have cleared source a for the same target")
	}
	if !d[reconcile.Cell{Skill: "deploy", Source: "b", Target: "t1"}] {
		t.Error("source b should be selected")
	}
}

func TestSelectCellDoesNotAffectOtherTargets(t *testing.T) {
	skills := []engine.AvailableSkill{{Name: "deploy", Source: "a"}, {Name: "deploy", Source: "b"}}
	d := map[reconcile.Cell]bool{}
	selectCell(d, reconcile.Cell{Skill: "deploy", Source: "a", Target: "t1"}, skills)
	selectCell(d, reconcile.Cell{Skill: "deploy", Source: "b", Target: "t2"}, skills)
	// Different targets: both stay selected.
	if !d[reconcile.Cell{Skill: "deploy", Source: "a", Target: "t1"}] ||
		!d[reconcile.Cell{Skill: "deploy", Source: "b", Target: "t2"}] {
		t.Error("selections in different targets must not interfere")
	}
}
