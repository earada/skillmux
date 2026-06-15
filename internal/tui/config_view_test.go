package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
)

func newConfigModel(t *testing.T, cfg *config.Config) (Model, *engine.Engine, string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	e := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, configPath, filepath.Join(t.TempDir(), "m.json"))
	return New(e), e, configPath
}

func key(kt tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: kt} }

func TestConfigAddTargetFlow(t *testing.T) {
	m, e, configPath := newConfigModel(t, &config.Config{})

	m, _ = step(t, m, runes("c")) // open config
	if m.mode != modeConfig {
		t.Fatalf("expected modeConfig, got %v", m.mode)
	}
	m, _ = step(t, m, runes("t")) // add-target form
	if m.mode != modeForm {
		t.Fatalf("expected modeForm, got %v", m.mode)
	}

	m, _ = step(t, m, runes("cc"))        // type name
	m, _ = step(t, m, key(tea.KeyTab))    // -> path field
	m, _ = step(t, m, runes("/tmp/skl"))  // type path
	m, _ = step(t, m, key(tea.KeyEnter))  // submit

	if m.mode != modeConfig {
		t.Fatalf("after save expected modeConfig, got %v", m.mode)
	}
	if len(e.Config.Targets) != 1 || e.Config.Targets[0].Name != "cc" || e.Config.Targets[0].Path != "/tmp/skl" {
		t.Fatalf("target not added in memory: %+v", e.Config.Targets)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Targets) != 1 {
		t.Errorf("target not persisted: %+v", reloaded.Targets)
	}
}

func TestConfigDeleteEntry(t *testing.T) {
	m, e, configPath := newConfigModel(t, &config.Config{
		Targets: []config.TargetEntry{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}},
	})
	m, _ = step(t, m, runes("c")) // open config; cursor at 0 (target "a")
	m, _ = step(t, m, runes("d")) // delete "a"

	if len(e.Config.Targets) != 1 || e.Config.Targets[0].Name != "b" {
		t.Fatalf("delete failed in memory: %+v", e.Config.Targets)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Targets) != 1 || reloaded.Targets[0].Name != "b" {
		t.Errorf("delete not persisted: %+v", reloaded.Targets)
	}
}

func TestConfigEscReturnsToMatrixAndRefreshes(t *testing.T) {
	m, _, _ := newConfigModel(t, &config.Config{Targets: []config.TargetEntry{{Name: "cc", Path: "/x"}}})
	m, _ = step(t, m, runes("c"))
	m, cmd := step(t, m, key(tea.KeyEsc))
	if m.mode != modeMatrix {
		t.Fatalf("esc should return to matrix, got %v", m.mode)
	}
	if cmd == nil {
		t.Error("leaving config should trigger a refresh")
	}
	if len(m.targets) != 1 || m.targets[0].Name != "cc" {
		t.Errorf("targets not reloaded into the model: %+v", m.targets)
	}
}

func TestConfigFormCancel(t *testing.T) {
	m, e, _ := newConfigModel(t, &config.Config{})
	m, _ = step(t, m, runes("c"))
	m, _ = step(t, m, runes("s")) // add-source form
	m, _ = step(t, m, runes("junk"))
	m, _ = step(t, m, key(tea.KeyEsc)) // cancel
	if m.mode != modeConfig {
		t.Fatalf("cancel should return to config, got %v", m.mode)
	}
	if len(e.Config.Sources) != 0 {
		t.Errorf("cancel should add nothing: %+v", e.Config.Sources)
	}
}

func TestConfigAddSourceInfersAndPersists(t *testing.T) {
	m, e, configPath := newConfigModel(t, &config.Config{})
	m, _ = step(t, m, runes("c"))
	m, _ = step(t, m, runes("s"))
	m, _ = step(t, m, runes("remote"))
	m, _ = step(t, m, key(tea.KeyTab))
	m, _ = step(t, m, runes("https://github.com/o/r"))
	m, _ = step(t, m, key(tea.KeyEnter))

	if len(e.Config.Sources) != 1 || e.Config.Sources[0].Name != "remote" {
		t.Fatalf("source not added: %+v", e.Config.Sources)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Sources) != 1 || reloaded.Sources[0].Location != "https://github.com/o/r" {
		t.Errorf("source not persisted: %+v", reloaded.Sources)
	}
}
