package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// catalogCache is the on-disk form of the last Refresh's available Skills.
// Persisting it lets the TUI render the matrix instantly on startup from the
// last-known fingerprints while a fresh Refresh runs in the background.
// Per-Source errors are not cached; they are transient to each Refresh.
type catalogCache struct {
	Skills []AvailableSkill `json:"skills"`
}

func (e *Engine) catalogPath() string {
	return filepath.Join(e.Fetcher.CacheDir, "catalog.json")
}

// CachedCatalog loads the last persisted catalog. A missing or unreadable cache
// yields an empty Catalog (startup simply waits for the first Refresh).
func (e *Engine) CachedCatalog() Catalog {
	cat := Catalog{SourceErrors: map[string]error{}}
	data, err := os.ReadFile(e.catalogPath())
	if err != nil {
		return cat
	}
	var c catalogCache
	if err := json.Unmarshal(data, &c); err != nil {
		return cat
	}
	cat.Skills = c.Skills
	return cat
}

// saveCatalog persists the catalog's Skills for the next startup. Best-effort:
// a cache write failure must not break a Refresh.
func (e *Engine) saveCatalog(cat Catalog) {
	data, err := json.MarshalIndent(catalogCache{Skills: cat.Skills}, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(e.catalogPath()), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(e.catalogPath(), data, 0o644)
}
