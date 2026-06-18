package tui

import (
	"sort"
	"strings"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
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

// skillEdge is one outgoing inter-Skill reference shown in the 'v' skill view: a
// Dependency or a Suggestion, with the Source that resolves it (and whether that
// crosses Sources). bulk marks an edge a router-wide `[[suggestion]]` (To empty)
// holds down — the per-edge toggle cannot lift it, so only the TOML can.
type skillEdge struct {
	to          string
	suggestion  bool
	crossSource bool
	source      string
	bulk        bool
}

// skillEdges resolves the cursor Skill's outgoing references into edges, sorted
// by target name. Dependencies and Suggestions are unioned (every detected Ref
// is one or the other) so the view lists the whole relationship at once.
func (m Model) skillEdges(sk engine.AvailableSkill) []skillEdge {
	g := m.eng.DependencyGraph(m.cat)
	offers := m.sourceOffers()
	bulk := m.eng.Config.HasBulkSuggestion(sk.Name)

	edges := make([]skillEdge, 0, len(g.Deps(sk.Name))+len(g.Suggests(sk.Name)))
	for _, d := range g.Deps(sk.Name) {
		src, cross := resolveOffer(offers[d], sk.Source)
		edges = append(edges, skillEdge{to: d, source: src, crossSource: cross})
	}
	for _, s := range g.Suggests(sk.Name) {
		src, cross := resolveOffer(offers[s], sk.Source)
		edges = append(edges, skillEdge{to: s, suggestion: true, source: src, crossSource: cross, bulk: bulk})
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].to < edges[j].to })
	return edges
}

// brokenEntry is one present cell whose Dependency closure is unsatisfied,
// paired with the missing members. The Plan's "broken" section is a sorted list
// of these.
type brokenEntry struct {
	Cell    reconcile.Cell
	Missing []engine.MissingDep
}

// brokenList returns every present cell (marked or installed) whose closure is
// unsatisfied under the current desired selection, sorted by Target then Skill
// then Source so the Plan reads deterministically. Mirrors brokenCells' presence
// gate so the Plan and the matrix agree on what is broken.
func (m Model) brokenList() []brokenEntry {
	breakages := m.eng.Breakages(m.cat, selected(m.desired))
	var out []brokenEntry
	for cell, missing := range breakages {
		if len(missing) == 0 {
			continue
		}
		st := m.status[statusKey{cell.Skill, cell.Source, cell.Target}]
		if m.cellPresent(cell, st) {
			out = append(out, brokenEntry{Cell: cell, Missing: missing})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Cell, out[j].Cell
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		if a.Skill != b.Skill {
			return a.Skill < b.Skill
		}
		return a.Source < b.Source
	})
	return out
}

// fixBroken is the Plan's `f` shortcut: it adds the resolvable missing closure of
// every broken cell to the desired selection, conflict-free. A dependency no
// Source offers (MissingDep.Source == "") cannot be installed, so it is left
// alone — it stays in the broken section as informational.
func (m *Model) fixBroken() {
	for _, e := range m.brokenList() {
		for _, md := range e.Missing {
			if md.Source == "" {
				continue // unresolvable: nothing to mark
			}
			selectCell(m.desired, reconcile.Cell{Skill: md.Name, Source: md.Source, Target: e.Cell.Target}, m.skills)
		}
	}
}

// fixable reports whether any broken entry has a dependency f could actually add
// — used to decide whether to offer the `f` key.
func fixable(broken []brokenEntry) bool {
	for _, e := range broken {
		for _, md := range e.Missing {
			if md.Source != "" {
				return true
			}
		}
	}
	return false
}

// markClosure is the matrix `d` shortcut: it marks the Skill under the cursor
// and its full transitive Dependency closure in the cursor cell's Target, so one
// keystroke cures the amber. Each cell is marked conflict-free (selectCell drops
// any rival Source of the same name), and the closure resolves each dependency
// to a concrete Source preferring the depending Skill's own. A dependency no
// Source offers is silently skipped — it cannot be installed, so there is
// nothing to mark.
func (m *Model) markClosure() {
	sk, ok := m.curSkill()
	if !ok || m.col < 0 || m.col >= len(m.targets) {
		return
	}
	target := m.targets[m.col].Name
	selectCell(m.desired, reconcile.Cell{Skill: sk.Name, Source: sk.Source, Target: target}, m.skills)
	g := m.eng.DependencyGraph(m.cat)
	for _, c := range g.ClosureCells(sk.Name, sk.Source, target) {
		selectCell(m.desired, c, m.skills)
	}
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
