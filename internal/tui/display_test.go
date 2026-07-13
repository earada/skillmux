package tui

import (
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

func TestSkillLabel(t *testing.T) {
	cases := []struct {
		name     string
		s        engine.AvailableSkill
		conflict bool
		want     []string // substrings that must appear
		deny     []string // substrings that must not appear
	}{
		{
			name: "plain skill shows name and source",
			s:    engine.AvailableSkill{Name: "deploy", Source: "local"},
			want: []string{"deploy", "(local)"},
			deny: []string{" / ", deprecatedGlyph},
		},
		{
			name: "grouped skill shows the name before its folder group",
			s:    engine.AvailableSkill{Name: "strict-mode", Source: "mp", Group: "typescript"},
			want: []string{"strict-mode", "typescript", "(mp)"},
		},
		{
			name: "deprecated skill carries the glyph",
			s:    engine.AvailableSkill{Name: "old", Source: "local", Deprecated: true},
			want: []string{deprecatedGlyph, "old"},
		},
		{
			name:     "conflicting name still renders",
			s:        engine.AvailableSkill{Name: "dup", Source: "a"},
			conflict: true,
			want:     []string{"dup", "(a)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := skillLabel(tc.s, tc.conflict)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("label %q missing %q", got, w)
				}
			}
			for _, d := range tc.deny {
				if strings.Contains(got, d) {
					t.Errorf("label %q should not contain %q", got, d)
				}
			}
		})
	}
}

func TestRenderGroupPreservesPathText(t *testing.T) {
	// Colour codes are stripped under the test renderer, but the visible path
	// text (including the reddened word) must survive intact and in order.
	got := renderGroup("legacy/deprecated/auth")
	for _, want := range []string{"legacy/", "deprecated", "/auth"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderGroup output %q missing %q", got, want)
		}
	}
}

func TestSkillLabelNameLeadsGroup(t *testing.T) {
	got := skillLabel(engine.AvailableSkill{Name: "strict-mode", Source: "mp", Group: "typescript"}, false)
	if strings.Index(got, "strict-mode") >= strings.Index(got, "typescript") {
		t.Errorf("name should come before the group in %q", got)
	}
}

func TestInstalledSkillsRiseToTop(t *testing.T) {
	// Alphabetically "build" < "deploy" < "ship"; install "ship" (last) and it
	// should jump ahead of the uninstalled skills regardless.
	e := testEngineSkills(t, "build", "deploy", "ship")
	cat := e.Refresh()
	if _, err := e.Apply(e.Preview([]reconcile.Cell{{Skill: "ship", Source: "local", Target: "cc"}}, cat), apply.Options{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	m := New(e).onRefreshed(e.Refresh())
	if len(m.skills) == 0 || m.skills[0].Name != "ship" {
		got := make([]string, len(m.skills))
		for i, s := range m.skills {
			got[i] = s.Name
		}
		t.Fatalf("installed skill should sort first, got %v", got)
	}
}

func TestMatrixDrawsSectionDividers(t *testing.T) {
	e := testEngineSkills(t, "cc") // gives one target; rows set manually below
	m := New(e)
	m.skills = []engine.AvailableSkill{
		{Name: "a", Source: "s"},                   // installed
		{Name: "b", Source: "s"},                   // not installed
		{Name: "c", Source: "s", Deprecated: true}, // deprecated
	}
	m.installed = map[skillRef]bool{{name: "a", source: "s"}: true}

	out := m.viewMatrix()
	// One header rule plus a full-width divider between each of the three
	// sections (installed | not-installed | deprecated) = three "├" lines.
	if got := strings.Count(out, "├"); got != 3 {
		t.Errorf("want 3 horizontal rules (header + 2 dividers), got %d:\n%s", got, out)
	}
	if strings.Contains(out, dividerMarker) {
		t.Errorf("invisible divider marker leaked into output:\n%s", out)
	}
}

func TestDeprecatedSkillsSinkToBottom(t *testing.T) {
	e := testEngineSkills(t, "cc") // engine wiring; catalog supplied directly below
	cat := engine.Catalog{
		SourceErrors: map[string]error{},
		Skills: []engine.AvailableSkill{
			{Name: "zeta", Source: "s"},
			{Name: "alpha", Source: "s", Deprecated: true},           // frontmatter flag
			{Name: "gamma", Source: "s", Group: "skills/deprecated"}, // deprecated by path
			{Name: "beta", Source: "s"},
		},
	}
	m := New(e).onRefreshed(cat)
	got := make([]string, len(m.skills))
	for i, s := range m.skills {
		got[i] = s.Name
	}
	// Live skills first (alphabetical), then deprecated — by flag or by path.
	want := []string{"beta", "zeta", "alpha", "gamma"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestRowsGroupBySourceThenName(t *testing.T) {
	e := testEngineSkills(t, "cc")
	cat := engine.Catalog{
		SourceErrors: map[string]error{},
		Skills: []engine.AvailableSkill{
			{Name: "zebra", Source: "alpha-src"},
			{Name: "apple", Source: "beta-src"},
			{Name: "mango", Source: "alpha-src"},
			{Name: "banana", Source: "beta-src"},
		},
	}
	m := New(e).onRefreshed(cat)
	var got []string
	for _, s := range m.skills {
		got = append(got, s.Source+"/"+s.Name)
	}
	// All not-installed, so one section: Sources stay together, names sorted.
	want := []string{"alpha-src/mango", "alpha-src/zebra", "beta-src/apple", "beta-src/banana"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestIsDeprecated(t *testing.T) {
	cases := []struct {
		s    engine.AvailableSkill
		want bool
	}{
		{engine.AvailableSkill{Name: "a"}, false},
		{engine.AvailableSkill{Name: "a", Deprecated: true}, true},
		{engine.AvailableSkill{Name: "a", Group: "skills/deprecated"}, true},
		{engine.AvailableSkill{Name: "a", Group: "Legacy/Deprecated/auth"}, true},
		{engine.AvailableSkill{Name: "a", Group: "skills/engineering"}, false},
	}
	for _, c := range cases {
		if got := isDeprecated(c.s); got != c.want {
			t.Errorf("isDeprecated(%+v) = %v, want %v", c.s, got, c.want)
		}
	}
}
