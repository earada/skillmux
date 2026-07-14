package tui

import (
	"sort"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

// initialDesired derives the starting desired selection from the current
// Status: a cell is desired when its Skill is already installed from that
// Source (up-to-date, update-available, or unavailable-but-still-installed), so
// the matrix opens reflecting reality. An unavailable installed cell starts
// desired so the default action is to keep it — reconcile no-ops it rather than
// uninstalling behind the user's back; deselecting it uninstalls deliberately.
func initialDesired(cells []engine.CellStatus) map[reconcile.Cell]bool {
	d := map[reconcile.Cell]bool{}
	for _, c := range cells {
		switch c.Status {
		case domain.StatusUpToDate, domain.StatusUpdateAvailable, domain.StatusUnavailable:
			d[reconcile.Cell{Skill: c.SkillName, Source: c.SourceName, Target: c.TargetName}] = true
		}
	}
	return d
}

// selected returns the cells marked desired, in a deterministic order.
func selected(d map[reconcile.Cell]bool) []reconcile.Cell {
	var out []reconcile.Cell
	for c, on := range d {
		if on {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		if out[i].Skill != out[j].Skill {
			return out[i].Skill < out[j].Skill
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// selectCell marks cell c as desired and, to keep the selection conflict-free,
// clears any other Source offering the same Skill name into the same Target.
// This is how a name Conflict is resolved in the declarative model: choosing
// one Source's cell deselects the rival Sources rather than producing a Plan
// that would fail at Apply.
func selectCell(d map[reconcile.Cell]bool, c reconcile.Cell, skills []engine.AvailableSkill) {
	for _, sk := range skills {
		if sk.Name == c.Skill && sk.Source != c.Source {
			d[reconcile.Cell{Skill: c.Skill, Source: sk.Source, Target: c.Target}] = false
		}
	}
	d[c] = true
}

// setRow selects or clears every Target for one Skill row (the All / None
// shortcuts). When selecting, it stays conflict-free via selectCell.
func setRow(d map[reconcile.Cell]bool, skill, source string, targets []domain.Target, skills []engine.AvailableSkill, on bool) {
	for _, t := range targets {
		c := reconcile.Cell{Skill: skill, Source: source, Target: t.Name}
		if on {
			selectCell(d, c, skills)
		} else {
			d[c] = false
		}
	}
}
