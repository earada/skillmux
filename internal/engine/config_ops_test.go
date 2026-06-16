package engine

import (
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
)

func cfgEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	e := New(&config.Config{}, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, configPath, filepath.Join(t.TempDir(), "m.json"))
	return e, configPath
}

func TestAddTargetPersists(t *testing.T) {
	e, configPath := cfgEngine(t)
	if err := e.AddTarget("cc", "/tmp/skills"); err != nil {
		t.Fatalf("AddTarget: %v", err)
	}
	if len(e.Config.Targets) != 1 {
		t.Fatalf("in-memory not updated: %+v", e.Config.Targets)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Targets) != 1 || reloaded.Targets[0].Name != "cc" {
		t.Errorf("not persisted: %+v", reloaded.Targets)
	}
}

func TestAddTargetRejectsDuplicateAndDoesNotPersist(t *testing.T) {
	e, configPath := cfgEngine(t)
	if err := e.AddTarget("cc", "/a"); err != nil {
		t.Fatal(err)
	}
	if err := e.AddTarget("cc", "/b"); err == nil {
		t.Fatal("expected duplicate error")
	}
	if len(e.Config.Targets) != 1 {
		t.Errorf("duplicate should not be added in memory: %+v", e.Config.Targets)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Targets) != 1 {
		t.Errorf("duplicate should not be persisted: %+v", reloaded.Targets)
	}
}

func TestUpdateTargetReplacesInPlaceAndPersists(t *testing.T) {
	e, configPath := cfgEngine(t)
	e.AddTarget("a", "/a")
	e.AddTarget("b", "/b")
	// Edit "a" (rename + new path) — it must keep its leading position.
	if err := e.UpdateTarget("a", "alpha", "/alpha"); err != nil {
		t.Fatalf("UpdateTarget: %v", err)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Targets) != 2 ||
		reloaded.Targets[0].Name != "alpha" || reloaded.Targets[0].Path != "/alpha" ||
		reloaded.Targets[1].Name != "b" {
		t.Errorf("update not persisted in place: %+v", reloaded.Targets)
	}
}

func TestUpdateTargetPathOnlyKeepsSameName(t *testing.T) {
	e, _ := cfgEngine(t)
	e.AddTarget("cc", "/old")
	if err := e.UpdateTarget("cc", "cc", "/new"); err != nil {
		t.Fatalf("editing the path with an unchanged name must not look like a duplicate: %v", err)
	}
	if e.Config.Targets[0].Path != "/new" {
		t.Errorf("path not updated: %+v", e.Config.Targets)
	}
}

func TestUpdateSourceReplacesInPlace(t *testing.T) {
	e, configPath := cfgEngine(t)
	e.AddSource(config.SourceEntry{Name: "s", Location: "/old"})
	if err := e.UpdateSource("s", config.SourceEntry{Name: "s", Location: "/new", Branch: "main"}); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Sources) != 1 || reloaded.Sources[0].Location != "/new" || reloaded.Sources[0].Branch != "main" {
		t.Errorf("source update not persisted: %+v", reloaded.Sources)
	}
}

func TestRemoveTargetPersists(t *testing.T) {
	e, configPath := cfgEngine(t)
	e.AddTarget("a", "/a")
	e.AddTarget("b", "/b")
	if err := e.RemoveTarget("a"); err != nil {
		t.Fatalf("RemoveTarget: %v", err)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Targets) != 1 || reloaded.Targets[0].Name != "b" {
		t.Errorf("remove not persisted correctly: %+v", reloaded.Targets)
	}
}

func TestAddAndRemoveSourcePersist(t *testing.T) {
	e, configPath := cfgEngine(t)
	if err := e.AddSource(config.SourceEntry{Name: "remote", Location: "https://github.com/o/r", Branch: "main"}); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Sources) != 1 || reloaded.Sources[0].Branch != "main" {
		t.Fatalf("source not persisted: %+v", reloaded.Sources)
	}
	if err := e.RemoveSource("remote"); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	reloaded, _ = config.Load(configPath)
	if len(reloaded.Sources) != 0 {
		t.Errorf("source not removed: %+v", reloaded.Sources)
	}
}

func TestMutateWithoutConfigPathErrors(t *testing.T) {
	e := New(&config.Config{}, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))
	if err := e.AddTarget("cc", "/a"); err == nil {
		t.Error("expected error when no config path is set")
	}
	if len(e.Config.Targets) != 0 {
		t.Error("nothing should be added when persistence is impossible")
	}
}
