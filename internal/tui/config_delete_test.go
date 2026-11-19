package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
)

// Deleting a config entry is a two-step confirm (skillmux-dqc): one stray 'd'
// must not rewrite config.toml.

func TestConfigDeleteFirstPressOnlyArms(t *testing.T) {
	m, e, configPath := newConfigModel(t, &config.Config{
		Targets: []config.TargetEntry{{Name: "claude-code", Path: "/a"}, {Name: "b", Path: "/b"}},
	})
	m, _ = step(t, m, runes("c")) // open config; cursor 0 -> target "claude-code"
	m, _ = step(t, m, runes("d"))

	if len(e.Config.Targets) != 2 {
		t.Fatalf("one press must not delete: %+v", e.Config.Targets)
	}
	if reloaded, err := config.Load(configPath); err == nil && len(reloaded.Targets) != 0 {
		t.Fatalf("one press must not write config.toml: %+v", reloaded.Targets)
	}
	if !strings.Contains(m.cfgMsg, `Delete target "claude-code"?`) || !strings.Contains(m.cfgMsg, "press d again") {
		t.Errorf("expected a confirm prompt, got %q", m.cfgMsg)
	}
	m.width, m.height = 120, 30
	if !strings.Contains(m.View(), `Delete target "claude-code"?`) {
		t.Errorf("prompt should be visible in the config view:\n%s", m.View())
	}
}

func TestConfigDeleteSecondPressRemoves(t *testing.T) {
	for _, k := range []string{"d", "x"} {
		m, e, configPath := newConfigModel(t, &config.Config{
			Targets: []config.TargetEntry{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}},
		})
		m, _ = step(t, m, runes("c"))
		m, _ = step(t, m, runes(k))
		m, _ = step(t, m, runes(k))

		if len(e.Config.Targets) != 1 || e.Config.Targets[0].Name != "b" {
			t.Fatalf("%s %s: delete failed in memory: %+v", k, k, e.Config.Targets)
		}
		reloaded, err := config.Load(configPath)
		if err != nil || len(reloaded.Targets) != 1 || reloaded.Targets[0].Name != "b" {
			t.Errorf("%s %s: delete not persisted: %+v (err %v)", k, k, reloaded, err)
		}
		if !strings.Contains(m.cfgMsg, "a") || strings.Contains(m.cfgMsg, "again") {
			t.Errorf("expected a deletion confirmation, got %q", m.cfgMsg)
		}
		if m.cfgDelArmed {
			t.Error("delete should disarm after it runs")
		}
	}
}

func TestConfigDeleteSourceSecondPressRemoves(t *testing.T) {
	m, e, _ := newConfigModel(t, &config.Config{
		Sources: []config.SourceEntry{{Name: "s1", Location: "/s"}},
	})
	m, _ = step(t, m, runes("c"))
	m, _ = step(t, m, runes("d"))
	if len(e.Config.Sources) != 1 || !strings.Contains(m.cfgMsg, `Delete source "s1"?`) {
		t.Fatalf("first press should only prompt: sources=%+v msg=%q", e.Config.Sources, m.cfgMsg)
	}
	m, _ = step(t, m, runes("d"))
	if len(e.Config.Sources) != 0 {
		t.Fatalf("second press should delete the source: %+v", e.Config.Sources)
	}
}

func TestConfigDeleteOtherKeyCancels(t *testing.T) {
	m, e, _ := newConfigModel(t, &config.Config{
		Targets: []config.TargetEntry{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}},
	})
	m, _ = step(t, m, runes("c"))
	m, _ = step(t, m, runes("d"))       // arm "a"
	m, _ = step(t, m, key(tea.KeyDown)) // cancels, moves to "b"
	if m.cfgDelArmed || strings.Contains(m.cfgMsg, "again") {
		t.Fatalf("other key should disarm and clear the prompt: msg=%q", m.cfgMsg)
	}
	if m.cfgCursor != 1 {
		t.Fatalf("the cancelling key should still be handled: cursor=%d", m.cfgCursor)
	}
	m, _ = step(t, m, runes("d")) // a fresh first press on "b" only arms
	if len(e.Config.Targets) != 2 {
		t.Fatalf("a d after cancelling must not delete: %+v", e.Config.Targets)
	}
	if !strings.Contains(m.cfgMsg, `"b"`) {
		t.Errorf("re-arm should target the entry under the cursor, got %q", m.cfgMsg)
	}
}

func TestConfigDeleteDisarmedByLeavingConfig(t *testing.T) {
	m, e, _ := newConfigModel(t, &config.Config{
		Targets: []config.TargetEntry{{Name: "a", Path: "/a"}},
	})
	m, _ = step(t, m, runes("c"))
	m, _ = step(t, m, runes("d"))
	m, _ = step(t, m, key(tea.KeyEsc)) // back to matrix
	if m.cfgDelArmed {
		t.Error("leaving config should disarm")
	}
	m.refreshing = false
	m, _ = step(t, m, runes("c"))
	m, _ = step(t, m, runes("d"))
	if len(e.Config.Targets) != 1 {
		t.Fatalf("a d after re-entering config must only arm: %+v", e.Config.Targets)
	}
}

func TestConfigDeleteReportsSaveFailure(t *testing.T) {
	cfg := &config.Config{Targets: []config.TargetEntry{{Name: "a", Path: "/a"}}}
	// No config path: every Remove* returns an error.
	e := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))
	m := New(e)
	m.refreshing = false

	m, _ = step(t, m, runes("c"))
	m, _ = step(t, m, runes("d"))
	m, _ = step(t, m, runes("d"))
	if !strings.HasPrefix(m.cfgMsg, "delete: ") {
		t.Errorf("a failed delete should be reported, got %q", m.cfgMsg)
	}
	if len(e.Config.Targets) != 1 {
		t.Errorf("failed delete should leave the target: %+v", e.Config.Targets)
	}
}

func TestConfigDeleteIgnoresDetectedRows(t *testing.T) {
	fakeToolHome(t, ".claude")
	m, _, _ := newConfigModel(t, &config.Config{})
	m, _ = step(t, m, runes("c")) // cursor 0 -> detected claude-code
	m, _ = step(t, m, runes("d"))
	if m.cfgDelArmed || m.cfgMsg != "" {
		t.Errorf("d on a detected row should do nothing: armed=%v msg=%q", m.cfgDelArmed, m.cfgMsg)
	}
}
