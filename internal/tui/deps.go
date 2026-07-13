package tui

import (
	"sort"
	"strings"

	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

// brokenCells reports which matrix cells should render amber. The SkillGraph
// already applies both presence rules — a Dependency is satisfied by name, but
// only the exact cell the user marked or installed reddens — so the keys of its
// breakage map are exactly the cells to redden.
func (m Model) brokenCells() map[reconcile.Cell]bool {
	out := map[reconcile.Cell]bool{}
	for cell := range m.graph.BrokenCells(selected(m.desired)) {
		out[cell] = true
	}
	return out
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

// skillEdges maps the graph's resolved edges for the Skill into the view's
// display struct. The graph already unions Dependencies and Suggestions, sorts
// them by name, and resolves each Source, so this is a straight projection.
func (m Model) skillEdges(sk engine.AvailableSkill) []skillEdge {
	edges := m.graph.Edges(sk.Name, sk.Source)
	out := make([]skillEdge, len(edges))
	for i, e := range edges {
		out[i] = skillEdge{
			to:          e.To,
			suggestion:  e.Kind == engine.Suggestion,
			crossSource: e.CrossSource,
			source:      e.Source,
			bulk:        e.Bulk,
		}
	}
	return out
}

// brokenEntry is one present cell whose Dependency closure is unsatisfied,
// paired with the missing members. The Plan's "broken" section is a sorted list
// of these.
type brokenEntry struct {
	Cell    reconcile.Cell
	Missing []engine.MissingDep
}

// brokenList returns every present cell whose closure is unsatisfied under the
// current desired selection, sorted by Target then Skill then Source so the Plan
// reads deterministically. The graph decides what is broken; the ordering is a
// presentation choice and stays here.
func (m Model) brokenList() []brokenEntry {
	breakages := m.graph.BrokenCells(selected(m.desired))
	out := make([]brokenEntry, 0, len(breakages))
	for cell, missing := range breakages {
		out = append(out, brokenEntry{Cell: cell, Missing: missing})
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
	for _, c := range m.graph.ClosureCells(sk.Name, sk.Source, target) {
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
	edges := m.graph.Edges(cur.Name, cur.Source)
	if len(edges) == 0 {
		return ""
	}

	// Direct deps unsatisfied in the cursor's Target, derived from the same
	// breakage map that reddens the matrix so the line and the cells agree. The
	// map only holds present cells, so an absent cursor cell yields no misses.
	missing := map[string]bool{}
	if m.col >= 0 && m.col < len(m.targets) {
		cell := reconcile.Cell{Skill: cur.Name, Source: cur.Source, Target: m.targets[m.col].Name}
		for _, md := range m.graph.BrokenCells(selected(m.desired))[cell] {
			missing[md.Name] = true
		}
	}

	var parts []string
	var deps, sugs []engine.Edge
	for _, e := range edges {
		if e.Kind == engine.Suggestion {
			sugs = append(sugs, e)
		} else {
			deps = append(deps, e)
		}
	}
	if len(deps) > 0 {
		items := make([]string, len(deps))
		for i, d := range deps {
			label := d.To
			if d.CrossSource {
				label += dimStyle.Render(" (" + d.Source + ")")
			}
			if missing[d.To] {
				label = brokenStyle.Render(label)
			}
			items[i] = label
		}
		parts = append(parts, dimStyle.Render("needs: ")+strings.Join(items, dimStyle.Render(", ")))
	}
	if len(sugs) > 0 {
		items := make([]string, len(sugs))
		for i, s := range sugs {
			items[i] = dimStyle.Render(s.To)
		}
		parts = append(parts, dimStyle.Render("suggests: ")+strings.Join(items, dimStyle.Render(", ")))
	}
	return strings.Join(parts, "    ")
}
