package tui

import (
	"sort"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

// initialDesired derives the starting desired selection from the current
// Status: a cell is desired when its Skill is already installed from that
// Source (up-to-date or update-available), so the matrix opens reflecting
// reality.
func initialDesired(cells []engine.CellStatus) map[reconcile.Cell]bool {
	d := map[reconcile.Cell]bool{}
	for _, c := range cells {
		if c.Status == domain.StatusUpToDate || c.Status == domain.StatusUpdateAvailable {
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

// setRow selects or clears every Target for one Skill row (the All / None
// shortcuts).
func setRow(d map[reconcile.Cell]bool, skill, source string, targets []domain.Target, on bool) {
	for _, t := range targets {
		d[reconcile.Cell{Skill: skill, Source: source, Target: t.Name}] = on
	}
}
