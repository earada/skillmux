package engine

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// concEngine builds an Engine over a persisted config with one local source
// offering a single "deploy" skill — enough for Refresh/Apply/config mutations
// to contend on the same Config, Manifest and clone.
func concEngine(t *testing.T) *Engine {
	t.Helper()
	srcRoot := t.TempDir()
	d := filepath.Join(srcRoot, "deploy")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: deploy\ndescription: d\n---\nv1"), 0o644)

	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(t.TempDir(), "m.json")
	return New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, configPath, mp)
}

// TestRefreshConfigCacheSerialized runs Refresh, ClearSourceCache and a config
// mutation concurrently. Without opMu the Config.Sources slice Refresh scans is
// the same one UpdateSource rewrites, so `go test -race` flags it; with the lock
// the operations serialise and the detector stays quiet (skillmux-3vj).
func TestRefreshConfigCacheSerialized(t *testing.T) {
	e := concEngine(t)
	loc := e.Config.Sources[0].Location // capture before racing goroutines read it

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); e.Refresh() }()
		go func() { defer wg.Done(); _, _ = e.ClearSourceCache("local") }()
		go func() {
			defer wg.Done()
			_ = e.UpdateSource("local", config.SourceEntry{Name: "local", Location: loc})
		}()
	}
	wg.Wait()
}

// TestConcurrentApplySerialized fires many Applies and Refreshes at once. Each
// Apply mutates the shared Manifest; without opMu concurrent Applies race (and
// can panic) on Manifest.Installations. The lock makes them one-at-a-time.
func TestConcurrentApplySerialized(t *testing.T) {
	e := concEngine(t)
	cat := e.Refresh()
	pre := e.Preview([]reconcile.Cell{{Skill: "deploy", Source: "local", Target: "cc"}}, cat)

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = e.Apply(pre, apply.Options{}) }()
		go func() { defer wg.Done(); e.Refresh() }()
	}
	wg.Wait()

	// The repeated identical installs must not have duplicated the manifest row.
	if got := len(e.Manifest.Installations); got != 1 {
		t.Fatalf("expected a single deploy installation, got %d: %+v", got, e.Manifest.Installations)
	}
}
