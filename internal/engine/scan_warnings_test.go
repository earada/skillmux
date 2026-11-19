package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRefreshSkipsBadSkillKeepsGoodOnes: one malformed SKILL.md in a Source
// must not fail the whole Source. The good Skill is fresh in the catalog, the
// bad one is reported as a warning, and the Source has no SourceError
// (skillmux-xol).
func TestRefreshSkipsBadSkillKeepsGoodOnes(t *testing.T) {
	e, _, skillDir, _ := newEnv(t)
	srcRoot := filepath.Dir(skillDir)
	bad := filepath.Join(srcRoot, "broken")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("no frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cat := e.Refresh()
	if len(cat.SourceErrors) != 0 {
		t.Fatalf("a single bad Skill must not fail the Source: %v", cat.SourceErrors)
	}
	if len(cat.Skills) != 1 || cat.Skills[0].Name != "deploy" {
		t.Fatalf("expected the good skill 'deploy' in the fresh catalog, got %+v", cat.Skills)
	}
	ws := cat.SourceWarnings["local"]
	if len(ws) != 1 || !strings.Contains(ws[0], filepath.Join("broken", "SKILL.md")) {
		t.Fatalf("expected one warning naming broken/SKILL.md, got %v", cat.SourceWarnings)
	}

	// Warnings are not persisted in the catalog cache.
	if cached := e.CachedCatalog(); len(cached.SourceWarnings) != 0 {
		t.Fatalf("warnings must not be persisted, got %v", cached.SourceWarnings)
	}
}
