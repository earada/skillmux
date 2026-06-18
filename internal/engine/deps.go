package engine

import (
	"sort"

	"github.com/earada/skillmux/internal/reconcile"
)

// DepGraph resolves the catalog's inferred references (AvailableSkill.Refs) into
// Dependency and Suggestion edges against the Config, and answers the
// transitive-closure and per-Target satisfaction questions the matrix needs.
// Edges are keyed by Skill name (identity); references from every Source that
// offers a name are unioned, so a Dependency surfaced for any instance of a name
// surfaces for the name.
type DepGraph struct {
	deps     map[string][]string // name -> dependency edge names (sorted, unique)
	suggests map[string][]string // name -> suggestion edge names (sorted, unique)
	offers   map[string][]string // name -> sources offering it (sorted, unique)
}

// DependencyGraph builds the graph for a catalog, classifying each reference as
// a Suggestion when the Config has reclassified that edge, otherwise as a
// Dependency.
func (e *Engine) DependencyGraph(cat Catalog) *DepGraph {
	depSet := map[string]map[string]bool{}
	sugSet := map[string]map[string]bool{}
	offSet := map[string]map[string]bool{}
	add := func(m map[string]map[string]bool, k, v string) {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		m[k][v] = true
	}
	for _, sk := range cat.Skills {
		add(offSet, sk.Name, sk.Source)
		for _, r := range sk.Refs {
			if e.Config.IsSuggestion(sk.Name, r) {
				add(sugSet, sk.Name, r)
			} else {
				add(depSet, sk.Name, r)
			}
		}
	}
	return &DepGraph{
		deps:     sortedSets(depSet),
		suggests: sortedSets(sugSet),
		offers:   sortedSets(offSet),
	}
}

func sortedSets(m map[string]map[string]bool) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, set := range m {
		vs := make([]string, 0, len(set))
		for v := range set {
			vs = append(vs, v)
		}
		sort.Strings(vs)
		out[k] = vs
	}
	return out
}

// Deps returns the direct Dependency edge names of a Skill.
func (g *DepGraph) Deps(name string) []string { return g.deps[name] }

// Suggests returns the direct Suggestion edge names of a Skill.
func (g *DepGraph) Suggests(name string) []string { return g.suggests[name] }

// Closure returns the transitive set of Dependency names a Skill needs, sorted
// and excluding itself. The walk carries a visited set so a dependency cycle
// (A→B→A) terminates instead of looping.
func (g *DepGraph) Closure(name string) []string {
	visited := map[string]bool{name: true}
	var queue []string
	queue = append(queue, g.deps[name]...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		queue = append(queue, g.deps[cur]...)
	}
	delete(visited, name)
	out := make([]string, 0, len(visited))
	for n := range visited {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// resolveSource picks a Source to install depName from, preferring the depending
// Skill's own Source so a same-named Skill from elsewhere is not silently
// substituted. crossSource is true when only a different Source offers the name.
// ok is false only if no Source offers it — which cannot happen for a detected
// reference, since detection matches against catalog names, but is handled
// defensively for transitive names.
func (g *DepGraph) resolveSource(depName, prefer string) (source string, crossSource, ok bool) {
	offers := g.offers[depName]
	if len(offers) == 0 {
		return "", false, false
	}
	for _, s := range offers {
		if s == prefer {
			return s, false, true
		}
	}
	return offers[0], true, true
}

// MissingDep is one unsatisfied member of a cell's Dependency closure: a Skill
// that must be present in the Target but is not, with the Source Skillmux would
// install it from and whether that resolution crosses Sources.
type MissingDep struct {
	Name        string
	Source      string
	CrossSource bool
}

// Breakages returns, for every catalog cell that is present in a Target
// (installed there or marked in desired), the unsatisfied members of its
// Dependency closure. A cell with no entry is satisfied. Suggestion edges are
// excluded by construction — they never enter a closure.
func (e *Engine) Breakages(cat Catalog, desired []reconcile.Cell) map[reconcile.Cell][]MissingDep {
	g := e.DependencyGraph(cat)
	present := e.presence(desired)
	out := map[reconcile.Cell][]MissingDep{}
	for _, sk := range cat.Skills {
		closure := g.Closure(sk.Name)
		if len(closure) == 0 {
			continue
		}
		for _, t := range e.Config.DomainTargets() {
			if !present[t.Name][sk.Name] {
				continue // the Skill itself is not here, so nothing to satisfy
			}
			var missing []MissingDep
			for _, dep := range closure {
				if present[t.Name][dep] {
					continue
				}
				src, cross, _ := g.resolveSource(dep, sk.Source)
				missing = append(missing, MissingDep{Name: dep, Source: src, CrossSource: cross})
			}
			if len(missing) > 0 {
				out[reconcile.Cell{Skill: sk.Name, Source: sk.Source, Target: t.Name}] = missing
			}
		}
	}
	return out
}

// presence reports, per Target, which Skill names are present — installed (per
// the Manifest) or marked in the desired selection. Satisfaction is by name,
// agnostic to Source: whatever occupies a name in a Target satisfies a
// Dependency on it.
func (e *Engine) presence(desired []reconcile.Cell) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	mark := func(target, name string) {
		if out[target] == nil {
			out[target] = map[string]bool{}
		}
		out[target][name] = true
	}
	for _, in := range e.Manifest.Installations {
		mark(in.TargetName, in.SkillName)
	}
	for _, c := range desired {
		mark(c.Target, c.Skill)
	}
	return out
}

// ClosureCells returns the desired Cells that install a Skill's full Dependency
// closure into target, each resolved to a concrete Source (preferring the
// Skill's own). The matrix 'd' key adds these alongside the Skill; the Plan 'f'
// key adds them to repair a broken selection. Adding an already-present Cell is
// a no-op for reconcile, so callers need not pre-filter.
func (g *DepGraph) ClosureCells(name, source, target string) []reconcile.Cell {
	var cells []reconcile.Cell
	for _, dep := range g.Closure(name) {
		src, _, ok := g.resolveSource(dep, source)
		if !ok {
			continue
		}
		cells = append(cells, reconcile.Cell{Skill: dep, Source: src, Target: target})
	}
	return cells
}
