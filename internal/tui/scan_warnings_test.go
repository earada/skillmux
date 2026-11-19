package tui

import (
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/engine"
)

// TestMatrixFooterShowsSkippedSkills: a Source's scan warnings surface in the
// matrix footer as a "skipped" note with the first reason, distinct from a
// hard Source error (skillmux-xol).
func TestMatrixFooterShowsSkippedSkills(t *testing.T) {
	e := testEngineSkills(t, "cc")
	cat := engine.Catalog{
		SourceErrors:   map[string]error{},
		SourceWarnings: map[string][]string{"s": {"broken/SKILL.md: missing YAML frontmatter", "other"}},
		Skills:         []engine.AvailableSkill{{Name: "a", Source: "s"}},
	}
	m := New(e).onRefreshed(cat)
	out := m.matrixFooter()
	if !strings.Contains(out, "2 skill(s) skipped") || !strings.Contains(out, "broken/SKILL.md: missing YAML frontmatter") {
		t.Errorf("footer missing skipped-skills note:\n%s", out)
	}
}
