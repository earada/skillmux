package engine

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/earada/skillmux/internal/domain"
)

// catalogCache is the on-disk form of the last Refresh's available Skills and
// Source Revisions. Persisting it lets the TUI render the matrix instantly on
// startup from the last-known fingerprints — and show each Source's last-known
// Revision — while a fresh Refresh runs in the background. Per-Source errors
// are not cached; they are transient to each Refresh.
type catalogCache struct {
	Skills    []AvailableSkill           `json:"skills"`
	Revisions map[string]domain.Revision `json:"revisions,omitempty"`
}

func (e *Engine) catalogPath() string {
	return filepath.Join(e.Fetcher.CacheDir, "catalog.json")
}

// CachedCatalog loads the last persisted catalog. A missing or unreadable cache
// yields an empty Catalog (startup simply waits for the first Refresh).
func (e *Engine) CachedCatalog() Catalog {
	cat := Catalog{
		Revisions:    map[string]domain.Revision{},
		SourceErrors: map[string]error{},
	}
	data, err := os.ReadFile(e.catalogPath())
	if err != nil {
		return cat
	}
	var c catalogCache
	if err := json.Unmarshal(data, &c); err != nil {
		return cat
	}
	cat.Skills = c.Skills
	if c.Revisions != nil {
		cat.Revisions = c.Revisions
	}
	return cat
}

// saveCatalog persists the catalog's Skills for the next startup. Best-effort:
// a cache write failure must not break a Refresh. The write is atomic — data is
// written to a temp file in the same directory and renamed into place — so a
// crash or partial write can never leave a truncated cache that would replace
// the last-known-good catalog with a corrupt snapshot (skillmux-ewq).
func (e *Engine) saveCatalog(cat Catalog) {
	data, err := json.MarshalIndent(catalogCache{Skills: cat.Skills, Revisions: cat.Revisions}, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(e.catalogPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "catalog-*.json.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, e.catalogPath()); err != nil {
		_ = os.Remove(tmpName)
	}
}
