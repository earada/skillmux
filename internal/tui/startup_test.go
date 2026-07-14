package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/apply"
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

func TestCachedInstalledSkillRemovedUpstreamStaysReconcilable(t *testing.T) {
	// Reproduces skillmux-crl: a skill is installed and cached, then removed from
	// its Source. On the next refresh its catalog row is gone but the manifest
	// still records it. The row must stay visible and reconcilable rather than
	// vanishing into a doomed Reinstall the user cannot cancel.
	srcRoot := t.TempDir()
	sdir := filepath.Join(srcRoot, "deploy")
	os.MkdirAll(sdir, 0o755)
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("---\nname: deploy\ndescription: d\n---\nv1"), 0o644)

	cacheDir := t.TempDir()
	targetPath := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "m.json")
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: targetPath}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}

	// First run: refresh (writes catalog cache) and install deploy.
	e1 := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: cacheDir}, "", manifestPath)
	cat := e1.Refresh()
	if _, err := e1.Apply(e1.Preview([]reconcile.Cell{{Skill: "deploy", Source: "local", Target: "cc"}}, cat), apply.Options{}); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Skill disappears upstream, then the app restarts over the same cache and
	// manifest: the cached row makes deploy start installed+desired.
	os.RemoveAll(sdir)
	man, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	e2 := engine.New(cfg, man, &fetch.Fetcher{CacheDir: cacheDir}, "", manifestPath)
	m := New(e2) // renders from cache: deploy present, desired=true
	desired := reconcile.Cell{Skill: "deploy", Source: "local", Target: "cc"}
	if !m.desired[desired] {
		t.Fatal("cached install should start desired")
	}

	// Background refresh lands: deploy is gone from the catalog.
	m = m.onRefreshed(e2.Refresh())

	// The row survives as an unavailable, still-installed entry.
	var row *engine.AvailableSkill
	for i := range m.skills {
		if m.skills[i].Name == "deploy" {
			row = &m.skills[i]
		}
	}
	if row == nil || !row.Unavailable {
		t.Fatalf("deploy row should survive as unavailable, skills = %+v", m.skills)
	}
	if !m.installed[skillRef{"deploy", "local"}] {
		t.Error("unavailable skill should still count as installed (visible section)")
	}
	if !m.desired[desired] {
		t.Error("keeping the unavailable skill: desired must stay true")
	}
	// Preview keeps it — no doomed Reinstall.
	if pre := e2.Preview(selected(m.desired), m.cat); len(pre.Plan.Operations) != 0 {
		t.Fatalf("keeping should be a no-op, got %+v", pre.Plan.Operations)
	}

	// The user can deselect the (now visible) row to uninstall it.
	m.desired[desired] = false
	pre := e2.Preview(selected(m.desired), m.cat)
	if len(pre.Plan.Operations) != 1 || pre.Plan.Operations[0].Kind != reconcile.Uninstall {
		t.Fatalf("deselect should yield one uninstall, got %+v", pre.Plan.Operations)
	}
	if _, err := e2.Apply(pre, apply.Options{}); err != nil {
		t.Fatalf("uninstall apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Error("skill should be uninstalled after deselect + apply")
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
