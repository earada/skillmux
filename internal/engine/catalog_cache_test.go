package engine

import (
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
)

func TestCachedCatalogEmptyWhenNoFile(t *testing.T) {
	e := New(&config.Config{}, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", "")
	if cat := e.CachedCatalog(); len(cat.Skills) != 0 {
		t.Errorf("expected empty cached catalog, got %+v", cat.Skills)
	}
}

func TestRefreshPersistsCatalogForInstantStartup(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cacheDir := e.Fetcher.CacheDir

	// First Refresh does the (local) scan and should cache the result.
	fresh := e.Refresh()
	if len(fresh.Skills) != 1 {
		t.Fatalf("expected 1 skill from refresh, got %+v", fresh.Skills)
	}

	// A brand-new Engine sharing the cache dir sees the catalog without any
	// scan/fetch — this is what makes startup instant.
	e2 := New(e.Config, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: cacheDir}, "", "")
	cached := e2.CachedCatalog()
	if len(cached.Skills) != 1 || cached.Skills[0].Name != "deploy" {
		t.Fatalf("cached catalog not loaded: %+v", cached.Skills)
	}
	if cached.Skills[0].Fingerprint == "" {
		t.Error("cached catalog should retain fingerprints")
	}
}
