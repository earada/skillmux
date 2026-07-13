package engine

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// graphEnv builds an Engine with the given Config and Manifest and no Fetcher —
// enough for the graph/closure/breakage queries, which run over a hand-built
// Catalog rather than a scan.
func graphEnv(cfg *config.Config, man *manifest.Manifest) *Engine {
	if man == nil {
		man = &manifest.Manifest{}
	}
	return New(cfg, man, nil, "", "")
}

func skill(name, source string, refs ...string) AvailableSkill {
	return AvailableSkill{Name: name, Source: source, Refs: refs}
}

func TestResolveRefsPopulatesFromFiles(t *testing.T) {
	srcRoot := t.TempDir()
	writeSkill(t, filepath.Join(srcRoot, "a"), "a", "A",
		"Use /b and see ../c/notes.md. Mentions /a itself and /unknown.")
	writeSkill(t, filepath.Join(srcRoot, "b"), "b", "B", "leaf")
	writeSkill(t, filepath.Join(srcRoot, "c"), "c", "C", "leaf")

	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	e := New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", "")
	cat := e.Refresh()

	byName := map[string][]string{}
	for _, s := range cat.Skills {
		byName[s.Name] = s.Refs
	}
	// /b is an invocation, ../c/ is a cross path; /a is self (excluded) and
	// /unknown names no Skill (ignored).
	if got := byName["a"]; !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("a.Refs = %v, want [b c]", got)
	}
	if len(byName["b"]) != 0 || len(byName["c"]) != 0 {
		t.Errorf("leaf skills should have no refs: b=%v c=%v", byName["b"], byName["c"])
	}
}

// edgeNames returns the To names of the edges of the given kind, sorted.
func edgeNames(edges []Edge, kind EdgeKind) []string {
	var out []string
	for _, e := range edges {
		if e.Kind == kind {
			out = append(out, e.To)
		}
	}
	sort.Strings(out)
	return out
}

func TestClosureTransitiveWithCycleGuard(t *testing.T) {
	cat := Catalog{Skills: []AvailableSkill{
		skill("a", "s", "b"),
		skill("b", "s", "c"),
		skill("c", "s", "a"), // cycle back to a
	}}
	g := graphEnv(&config.Config{}, nil).SkillGraph(cat)
	if got := g.closure("a"); !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("closure(a) = %v, want [b c] (self excluded, no infinite loop)", got)
	}
}

func TestSuggestionExcludedFromClosure(t *testing.T) {
	cat := Catalog{Skills: []AvailableSkill{
		skill("a", "s", "b", "c"),
		skill("b", "s"),
		skill("c", "s"),
	}}
	cfg := &config.Config{Suggestions: []config.SuggestionEntry{{From: "a", To: "c"}}}
	g := graphEnv(cfg, nil).SkillGraph(cat)

	edges := g.Edges("a", "s")
	if got := edgeNames(edges, Dependency); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("dependency edges of a = %v, want [b]", got)
	}
	if got := edgeNames(edges, Suggestion); !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("suggestion edges of a = %v, want [c]", got)
	}
	if got := g.closure("a"); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("closure(a) = %v, want [b] (suggestion c excluded)", got)
	}
}

func TestBreakagesPerTarget(t *testing.T) {
	cat := Catalog{Skills: []AvailableSkill{
		skill("a", "s", "b"),
		skill("b", "s"),
	}}
	cfg := &config.Config{Targets: []config.TargetEntry{
		{Name: "cc", Path: "/x"}, {Name: "cursor", Path: "/y"},
	}}
	g := graphEnv(cfg, nil).SkillGraph(cat)

	// a is desired in cc but b is not → cc cell is broken; cursor is clean.
	desired := []reconcile.Cell{{Skill: "a", Source: "s", Target: "cc"}}
	br := g.BrokenCells(desired)
	cc := reconcile.Cell{Skill: "a", Source: "s", Target: "cc"}
	if miss, ok := br[cc]; !ok || len(miss) != 1 || miss[0].Name != "b" {
		t.Fatalf("expected cc/a missing [b], got %+v", br)
	}
	if _, ok := br[reconcile.Cell{Skill: "a", Source: "s", Target: "cursor"}]; ok {
		t.Error("cursor cell should not be broken (a not present there)")
	}

	// Marking b in cc too satisfies the closure.
	desired = append(desired, reconcile.Cell{Skill: "b", Source: "s", Target: "cc"})
	if br := g.BrokenCells(desired); len(br) != 0 {
		t.Errorf("expected no breakages once b is marked, got %+v", br)
	}
}

func TestBreakageSatisfiedByInstalled(t *testing.T) {
	cat := Catalog{Skills: []AvailableSkill{skill("a", "s", "b"), skill("b", "s")}}
	cfg := &config.Config{Targets: []config.TargetEntry{{Name: "cc", Path: "/x"}}}
	// b already installed in cc (from any Source) satisfies a's dependency.
	man := &manifest.Manifest{Installations: []domain.Installation{
		{SkillName: "b", TargetName: "cc", SourceName: "other"},
	}}
	g := graphEnv(cfg, man).SkillGraph(cat)
	desired := []reconcile.Cell{{Skill: "a", Source: "s", Target: "cc"}}
	if br := g.BrokenCells(desired); len(br) != 0 {
		t.Errorf("installed b should satisfy the dependency, got %+v", br)
	}
}

func TestBreakageCrossSourceFlag(t *testing.T) {
	cfg := &config.Config{Targets: []config.TargetEntry{{Name: "cc", Path: "/x"}}}
	desired := []reconcile.Cell{{Skill: "a", Source: "s1", Target: "cc"}}

	// shared is offered only by s2, but a comes from s1 → cross-Source.
	cat := Catalog{Skills: []AvailableSkill{
		skill("a", "s1", "shared"),
		skill("shared", "s2"),
	}}
	br := graphEnv(cfg, nil).SkillGraph(cat).BrokenCells(desired)
	miss := br[reconcile.Cell{Skill: "a", Source: "s1", Target: "cc"}]
	if len(miss) != 1 || miss[0].Name != "shared" || miss[0].Source != "s2" || !miss[0].CrossSource {
		t.Fatalf("expected cross-Source miss from s2, got %+v", miss)
	}

	// Now s1 also offers shared → prefer the same Source, not cross.
	cat.Skills = append(cat.Skills, skill("shared", "s1"))
	br = graphEnv(cfg, nil).SkillGraph(cat).BrokenCells(desired)
	miss = br[reconcile.Cell{Skill: "a", Source: "s1", Target: "cc"}]
	if len(miss) != 1 || miss[0].Source != "s1" || miss[0].CrossSource {
		t.Fatalf("expected same-Source resolution from s1, got %+v", miss)
	}
}

func TestClosureCellsResolvesWholeClosure(t *testing.T) {
	cat := Catalog{Skills: []AvailableSkill{
		skill("a", "s", "b"),
		skill("b", "s", "c"),
		skill("c", "s"),
	}}
	g := graphEnv(&config.Config{}, nil).SkillGraph(cat)
	cells := g.ClosureCells("a", "s", "cc")
	got := make([]string, len(cells))
	for i, c := range cells {
		if c.Target != "cc" || c.Source != "s" {
			t.Errorf("cell %+v: wrong target/source", c)
		}
		got[i] = c.Skill
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("ClosureCells skills = %v, want [b c]", got)
	}
}
