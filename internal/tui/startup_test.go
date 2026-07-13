package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

func TestNewRendersInstantlyFromCachedCatalog(t *testing.T) {
	// Build a source + a shared cache dir, run one refresh to populate the
	// catalog cache, then a fresh engine over the same cache.
	srcRoot := t.TempDir()
	sdir := filepath.Join(srcRoot, "deploy")
	os.MkdirAll(sdir, 0o755)
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("---\nname: deploy\ndescription: d\n---\nv1"), 0o644)

	cacheDir := t.TempDir()
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	e1 := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: cacheDir}, "", filepath.Join(t.TempDir(), "m.json"))
	e1.Refresh() // populates cacheDir/catalog.json

	e2 := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: cacheDir}, "", filepath.Join(t.TempDir(), "m.json"))
	m := New(e2) // must render from cache without waiting for a refresh

	if len(m.skills) != 1 || m.skills[0].Name != "deploy" {
		t.Fatalf("model not populated from cached catalog: %+v", m.skills)
	}
	if !m.refreshing {
		t.Error("a background refresh should still be pending after startup")
	}
}

func TestConflictResolvedByExclusiveSelection(t *testing.T) {
	// Two sources offering the same skill name into one target.
	srcA := t.TempDir()
	srcB := t.TempDir()
	for _, root := range []string{srcA, srcB} {
		d := filepath.Join(root, "deploy")
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: deploy\ndescription: d\n---\nx"), 0o644)
	}
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
		Sources: []config.SourceEntry{{Name: "a", Location: srcA}, {Name: "b", Location: srcB}},
	}
	e := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))
	m := New(e).onRefreshed(e.Refresh())

	if len(m.skills) != 2 {
		t.Fatalf("expected two rows (deploy from a and b), got %+v", m.skills)
	}
	// Select row 0's cell, then row 1's cell, same target.
	m, _ = step(t, m, runes(" ")) // row 0 (deploy from a or b — sorted by source)
	m.row = 1
	m, _ = step(t, m, runes(" ")) // row 1, same target column

	sel := selected(m.desired)
	if len(sel) != 1 {
		t.Fatalf("exclusive selection should leave exactly one cell, got %+v", sel)
	}
	// The plan must therefore contain no Conflict op.
	plan := e.Preview(sel, m.cat).Plan
	for _, op := range plan.Operations {
		if op.Kind == reconcile.Conflict {
			t.Errorf("exclusive selection should not yield a conflict: %+v", plan.Operations)
		}
	}
}
