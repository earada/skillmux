package tui

import (
	"sort"
	"strings"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/reconcile"
)

// brokenCells reports which matrix cells should render amber: a cell that is
// present in its Target — installed there or marked in the desired selection —
// whose transitive Dependency closure is unsatisfied. Suggestion edges never
// enter a closure, so they never break a cell. Problem-first: every other cell
// stays clean.
//
// The engine flags a breakage by Skill *name* (installed reality wins over
// catalog identity), so a name present from one Source surfaces the same
// closure for that name's other Source rows too. We gate on this exact cell's
// own presence so only the cell the user actually marked/installed reddens, not
// its idle same-named siblings.
func (m Model) brokenCells() map[reconcile.Cell]bool {
	breakages := m.eng.Breakages(m.cat, selected(m.desired))
	out := map[reconcile.Cell]bool{}
	for cell, missing := range breakages {
		if len(missing) == 0 {
			continue
		}
		st := m.status[statusKey{cell.Skill, cell.Source, cell.Target}]
		if m.cellPresent(cell, st) {
			out[cell] = true
		}
	}
	return out
}

// cellPresent reports whether this exact (Skill, Source, Target) cell counts as
// present: marked in the desired selection, or installed there (up-to-date or
// update-available).
func (m Model) cellPresent(c reconcile.Cell, st domain.Status) bool {
	return m.desired[c] || st == domain.StatusUpToDate || st == domain.StatusUpdateAvailable
}

// depDetail renders the cursor row's dependency detail line: a `needs:` list of
// the Skill's direct Dependency edges, each amber when it is unsatisfied in the
// cursor's Target (matching the matrix), and a `suggests:` list of its
// Suggestion edges in neutral colour — Suggestions never warn. A dependency the
// cursor Skill's own Source cannot supply is flagged with the Source that
// resolves it. Returns "" when the Skill has neither.
func (m Model) depDetail() string {
	cur, ok := m.curSkill()
	if !ok {
		return ""
	}
	g := m.eng.DependencyGraph(m.cat)
	deps, sugs := g.Deps(cur.Name), g.Suggests(cur.Name)
	if len(deps) == 0 && len(sugs) == 0 {
		return ""
	}

	// Direct deps unsatisfied in the cursor's Target, derived from the same
	// breakage map that reddens the matrix so the line and the cells agree.
	missing := map[string]bool{}
	if m.col >= 0 && m.col < len(m.targets) {
		cell := reconcile.Cell{Skill: cur.Name, Source: cur.Source, Target: m.targets[m.col].Name}
		st := m.status[statusKey{cur.Name, cur.Source, m.targets[m.col].Name}]
		if m.cellPresent(cell, st) {
			for _, md := range m.eng.Breakages(m.cat, selected(m.desired))[cell] {
				missing[md.Name] = true
			}
		}
	}

	offers := m.sourceOffers()
	var parts []string
	if len(deps) > 0 {
		items := make([]string, len(deps))
		for i, d := range deps {
			label := d
			if src, cross := resolveOffer(offers[d], cur.Source); cross {
				label += dimStyle.Render(" (" + src + ")")
			}
			if missing[d] {
				label = brokenStyle.Render(label)
			}
			items[i] = label
		}
		parts = append(parts, dimStyle.Render("needs: ")+strings.Join(items, dimStyle.Render(", ")))
	}
	if len(sugs) > 0 {
		items := make([]string, len(sugs))
		for i, s := range sugs {
			items[i] = dimStyle.Render(s)
		}
		parts = append(parts, dimStyle.Render("suggests: ")+strings.Join(items, dimStyle.Render(", ")))
	}
	return strings.Join(parts, "    ")
}

// sourceOffers maps each Skill name to the Sources offering it (sorted), so the
// detail line can tell whether a dependency must be drawn from a Source other
// than the depending Skill's own.
func (m Model) sourceOffers() map[string][]string {
	set := map[string]map[string]bool{}
	for _, s := range m.skills {
		if set[s.Name] == nil {
			set[s.Name] = map[string]bool{}
		}
		set[s.Name][s.Source] = true
	}
	out := make(map[string][]string, len(set))
	for name, srcs := range set {
		for src := range srcs {
			out[name] = append(out[name], src)
		}
		sort.Strings(out[name])
	}
	return out
}

// resolveOffer picks the Source a dependency resolves to, preferring the
// depending Skill's own. cross is true when only a different Source offers it —
// the cross-Source resolution the detail line flags. Mirrors the engine's
// resolveSource so the UI agrees with what Apply would install.
func resolveOffer(sources []string, prefer string) (src string, cross bool) {
	if len(sources) == 0 {
		return "", false
	}
	for _, s := range sources {
		if s == prefer {
			return s, false
		}
	}
	return sources[0], true
}
