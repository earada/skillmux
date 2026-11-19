package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
)

// seedClone fakes a cached clone for a GitHub Source (no network): a dir at
// the Fetcher's cache key with a .git dir and a marker file inside.
func seedClone(t *testing.T, e *Engine, name string) string {
	t.Helper()
	dir := e.Fetcher.CacheDirFor(domain.Source{Name: name, Kind: domain.SourceGitHub})
	if dir == "" {
		t.Fatal("expected a cache dir for a GitHub Source")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func ghEntry(name string) config.SourceEntry {
	return config.SourceEntry{Name: name, Location: "https://github.com/acme/" + name}
}

func cacheEngine(t *testing.T, sources ...config.SourceEntry) *Engine {
	t.Helper()
	e, _ := cfgEngine(t)
	for _, s := range sources {
		if err := e.AddSource(s); err != nil {
			t.Fatalf("AddSource: %v", err)
		}
	}
	return e
}

func TestRemoveSourceDeletesCache(t *testing.T) {
	e := cacheEngine(t, ghEntry("alpha"), ghEntry("beta"))
	alpha := seedClone(t, e, "alpha")
	beta := seedClone(t, e, "beta")

	if err := e.RemoveSource("alpha"); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	if exists(alpha) {
		t.Errorf("removed Source's cache %s still on disk", alpha)
	}
	if !exists(filepath.Join(beta, "marker")) {
		t.Errorf("other Source's cache was touched")
	}
}

func TestRemoveLocalSourceIsFine(t *testing.T) {
	e := cacheEngine(t, config.SourceEntry{Name: "loc", Location: t.TempDir()})
	if err := e.RemoveSource("loc"); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
}

func TestRenameSourceMovesCache(t *testing.T) {
	e := cacheEngine(t, ghEntry("alpha"))
	oldDir := seedClone(t, e, "alpha")

	renamed := ghEntry("alpha")
	renamed.Name = "gamma"
	if err := e.UpdateSource("alpha", renamed); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	newDir := e.Fetcher.CacheDirFor(domain.Source{Name: "gamma", Kind: domain.SourceGitHub})
	if exists(oldDir) {
		t.Errorf("old cache %s still on disk after rename", oldDir)
	}
	if !exists(filepath.Join(newDir, ".git")) || !exists(filepath.Join(newDir, "marker")) {
		t.Errorf("clone not moved to new key %s", newDir)
	}
}

func TestRenameSourceKeepsExistingNewCache(t *testing.T) {
	e := cacheEngine(t, ghEntry("alpha"))
	oldDir := seedClone(t, e, "alpha")
	newDir := e.Fetcher.CacheDirFor(domain.Source{Name: "gamma", Kind: domain.SourceGitHub})
	if err := os.MkdirAll(filepath.Join(newDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	renamed := ghEntry("alpha")
	renamed.Name = "gamma"
	if err := e.UpdateSource("alpha", renamed); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	if exists(oldDir) {
		t.Errorf("old cache %s should be removed", oldDir)
	}
	if !exists(filepath.Join(newDir, ".git")) {
		t.Errorf("existing cache at new key was clobbered")
	}
	if exists(filepath.Join(newDir, "marker")) {
		t.Errorf("old clone should not overwrite the existing new-key cache")
	}
}

func TestLocationOnlyEditLeavesCache(t *testing.T) {
	e := cacheEngine(t, ghEntry("alpha"))
	dir := seedClone(t, e, "alpha")

	moved := ghEntry("alpha")
	moved.Location = "https://github.com/other/alpha"
	moved.Branch = "dev"
	if err := e.UpdateSource("alpha", moved); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	if !exists(filepath.Join(dir, "marker")) {
		t.Errorf("location-only edit should leave the cache alone")
	}
}

func TestFailedSaveLeavesCache(t *testing.T) {
	e := cacheEngine(t, ghEntry("alpha"), ghEntry("beta"))
	alpha := seedClone(t, e, "alpha")
	beta := seedClone(t, e, "beta")

	// Renaming alpha to beta duplicates a name: config.Save rejects it.
	dup := ghEntry("alpha")
	dup.Name = "beta"
	if err := e.UpdateSource("alpha", dup); err == nil {
		t.Fatal("expected duplicate-name error")
	}
	if !exists(filepath.Join(alpha, "marker")) || !exists(filepath.Join(beta, "marker")) {
		t.Errorf("failed save must leave caches untouched")
	}
	if len(e.Config.Sources) != 2 || e.Config.Sources[0].Name != "alpha" {
		t.Errorf("config not rolled back: %+v", e.Config.Sources)
	}
}

func TestRemoveSourceWithoutConfigPathLeavesCache(t *testing.T) {
	e := cacheEngine(t, ghEntry("alpha"))
	dir := seedClone(t, e, "alpha")
	e.configPath = ""
	if err := e.RemoveSource("alpha"); err == nil {
		t.Fatal("expected error without config path")
	}
	if !exists(filepath.Join(dir, "marker")) {
		t.Errorf("failed removal must leave the cache untouched")
	}
}
