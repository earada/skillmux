package engine

import (
	"sort"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// EdgeKind classifies an inter-Skill reference as a hard Dependency or a soft
// Suggestion. See CONTEXT.md.
type EdgeKind int

const (
	// Dependency is a hard edge: the referenced Skill must be present in the
	// Target or the depending Skill's cell is Unsatisfied.
	Dependency EdgeKind = iota
	// Suggestion is a soft, inert edge: shown but never Unsatisfied, never in a
	// closure, never in the Plan.
	Suggestion
)

// Edge is one outgoing inter-Skill reference from a Skill, fully resolved: the
// referenced name, whether it is a Dependency or a Suggestion, the Source that
// would satisfy it (preferring the depending Skill's own), whether that
// resolution crosses Sources, and — for a Suggestion — whether a router-wide
// bulk [[suggestion]] holds it down (which a per-edge toggle cannot lift).
type Edge struct {
	To          string
	Kind        EdgeKind
	Source      string
	CrossSource bool
	Bulk        bool
}

// MissingDep is one unsatisfied member of a cell's Dependency closure: a Skill
// that must be present in the Target but is not, with the Source Skillmux would
// install it from and whether that resolution crosses Sources.
type MissingDep struct {
	Name        string
	Source      string
	CrossSource bool
}

// SkillGraph answers every dependency question the matrix asks, built once per
// Catalog. It resolves the catalog's inferred references (AvailableSkill.Refs)
// into Dependency and Suggestion edges against the Config, precomputes each
// Skill's transitive Dependency closure inputs, and captures the installed
// reality from the Manifest — so edge resolution, closure walking, and both
// notions of presence live behind one interface.
//
// References are keyed by Skill name (identity); references from every Source
// that offers a name are unioned, so an edge surfaced for any instance of a
// name surfaces for the name. The desired selection is passed per query (it
// changes every keystroke); everything else is fixed at construction.
type SkillGraph struct {
	deps     map[string][]string // name -> dependency edge names (sorted, unique)
	suggests map[string][]string // name -> suggestion edge names (sorted, unique)
	offers   map[string][]string // name -> sources offering it (sorted, unique)
	bulk     map[string]bool     // name -> a router-wide bulk suggestion holds it
	targets  []string            // configured Target names

	// installed[target][name] records that the Manifest holds name in target
	// from *some* Source — installed reality that satisfies a Dependency by
	// name. installedSrc[target][name] is *which* Source, used to decide whether
	// a specific (Skill, Source) cell is itself present.
	installed    map[string]map[string]bool
	installedSrc map[string]map[string]string
}

// SkillGraph builds the graph for a catalog against the Engine's current Config
// and Manifest. A thin convenience wrapper over NewSkillGraph.
func (e *Engine) SkillGraph(cat Catalog) *SkillGraph {
	return NewSkillGraph(cat, e.Config, e.Manifest)
}

// NewSkillGraph builds the graph from explicit inputs. Pure over its arguments:
// it reads the catalog's references, classifies each against cfg, and snapshots
// man's Installations — creating nothing and retaining neither cat nor man.
func NewSkillGraph(cat Catalog, cfg *config.Config, man *manifest.Manifest) *SkillGraph {
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
			if cfg.IsSuggestion(sk.Name, r) {
				add(sugSet, sk.Name, r)
			} else {
				add(depSet, sk.Name, r)
			}
		}
	}
	bulk := map[string]bool{}
	for name := range offSet {
		if cfg.HasBulkSuggestion(name) {
			bulk[name] = true
		}
	}

	g := &SkillGraph{
		deps:         sortedSets(depSet),
		suggests:     sortedSets(sugSet),
		offers:       sortedSets(offSet),
		bulk:         bulk,
		installed:    map[string]map[string]bool{},
		installedSrc: map[string]map[string]string{},
	}
	for _, t := range cfg.DomainTargets() {
		g.targets = append(g.targets, t.Name)
	}
	if man != nil {
		for _, in := range man.Installations {
			if g.installed[in.TargetName] == nil {
				g.installed[in.TargetName] = map[string]bool{}
				g.installedSrc[in.TargetName] = map[string]string{}
			}
			g.installed[in.TargetName][in.SkillName] = true
			g.installedSrc[in.TargetName][in.SkillName] = in.SourceName
		}
	}
	return g
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

// Edges returns the direct outgoing references of a Skill — Dependencies then
// Suggestions unioned, sorted by target name — each resolved to a concrete
// Source (preferring the depending Skill's own, given by source) with its
// CrossSource flag set. Suggestion edges carry the router-wide Bulk flag. This
// is the whole relationship the skill view and the cursor detail line render.
func (g *SkillGraph) Edges(name, source string) []Edge {
	edges := make([]Edge, 0, len(g.deps[name])+len(g.suggests[name]))
	for _, d := range g.deps[name] {
		src, cross := g.resolve(d, source)
		edges = append(edges, Edge{To: d, Kind: Dependency, Source: src, CrossSource: cross})
	}
	for _, s := range g.suggests[name] {
		src, cross := g.resolve(s, source)
		edges = append(edges, Edge{To: s, Kind: Suggestion, Source: src, CrossSource: cross, Bulk: g.bulk[name]})
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].To < edges[j].To })
	return edges
}

// BrokenCells reports every cell that should surface as Unsatisfied: a cell
// present in its Target — installed there or marked in desired — whose
// transitive Dependency closure is not fully present. The keys are exactly the
// cells to redden; each value lists the missing closure members.
//
// Two notions of presence live here, deliberately distinct. A Dependency is
// *satisfied* by name — whatever Source occupies the name in the Target counts
// (installed reality wins). But only the exact (Skill, Source) cell the user
// marked or installed *reddens*, so an idle same-named sibling row stays clean.
func (g *SkillGraph) BrokenCells(desired []reconcile.Cell) map[reconcile.Cell][]MissingDep {
	// present-by-name: Manifest (any Source) + desired (any Source), per Target —
	// the satisfaction test for a closure member.
	present := map[string]map[string]bool{}
	mark := func(target, name string) {
		if present[target] == nil {
			present[target] = map[string]bool{}
		}
		present[target][name] = true
	}
	for t, names := range g.installed {
		for n := range names {
			mark(t, n)
		}
	}
	desiredCell := map[reconcile.Cell]bool{}
	for _, c := range desired {
		mark(c.Target, c.Skill)
		desiredCell[c] = true
	}

	out := map[reconcile.Cell][]MissingDep{}
	for name, sources := range g.offers {
		closure := g.closure(name)
		if len(closure) == 0 {
			continue
		}
		for _, source := range sources {
			for _, target := range g.targets {
				cell := reconcile.Cell{Skill: name, Source: source, Target: target}
				if !g.cellPresent(cell, desiredCell) {
					continue // this exact cell is not here — it never reddens
				}
				var missing []MissingDep
				for _, dep := range closure {
					if present[target][dep] {
						continue
					}
					src, cross := g.resolve(dep, source)
					missing = append(missing, MissingDep{Name: dep, Source: src, CrossSource: cross})
				}
				if len(missing) > 0 {
					out[cell] = missing
				}
			}
		}
	}
	return out
}

// cellPresent reports whether the exact (Skill, Source, Target) cell counts as
// present: marked in desired, or installed in that Target from that Source.
func (g *SkillGraph) cellPresent(c reconcile.Cell, desired map[reconcile.Cell]bool) bool {
	if desired[c] {
		return true
	}
	return g.installedSrc[c.Target][c.Skill] == c.Source
}

// ClosureCells returns the desired Cells that install a Skill's full transitive
// Dependency closure into target, each resolved to a concrete Source (preferring
// the Skill's own). A dependency no Source offers is skipped — it cannot be
// installed. Adding an already-present Cell is a no-op for reconcile, so callers
// need not pre-filter.
func (g *SkillGraph) ClosureCells(name, source, target string) []reconcile.Cell {
	var cells []reconcile.Cell
	for _, dep := range g.closure(name) {
		src, _ := g.resolve(dep, source)
		if src == "" {
			continue
		}
		cells = append(cells, reconcile.Cell{Skill: dep, Source: src, Target: target})
	}
	return cells
}

// closure returns the transitive set of Dependency names a Skill needs, sorted
// and excluding itself. The walk carries a visited set so a dependency cycle
// (A→B→A) terminates instead of looping. Suggestion edges are absent from
// g.deps by construction, so they never enter a closure.
func (g *SkillGraph) closure(name string) []string {
	visited := map[string]bool{name: true}
	queue := append([]string(nil), g.deps[name]...)
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

// resolve picks a Source to satisfy name from, preferring prefer so a same-named
// Skill from elsewhere is not silently substituted. cross is true when only a
// different Source offers it. source is "" only when no Source offers the name —
// which cannot happen for a detected reference, but is handled defensively for
// transitive names.
func (g *SkillGraph) resolve(name, prefer string) (source string, cross bool) {
	offers := g.offers[name]
	if len(offers) == 0 {
		return "", false
	}
	for _, s := range offers {
		if s == prefer {
			return s, false
		}
	}
	return offers[0], true
}
