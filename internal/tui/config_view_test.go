package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
)

func TestConfigViewShowsSourceRevision(t *testing.T) {
	m, _, _ := newConfigModel(t, &config.Config{
		Sources: []config.SourceEntry{{Name: "remote", Location: "https://github.com/o/r"}},
	})
	m.width, m.height = 100, 24
	m.cat.Revisions = map[string]domain.Revision{"remote": {Ref: "main", ShortSHA: "a1b2c3d"}}

	m, _ = step(t, m, runes("c")) // open config
	if !strings.Contains(m.View(), "main @ a1b2c3d") {
		t.Errorf("config view should show the source revision; got:\n%s", m.View())
	}
}

func newConfigModel(t *testing.T, cfg *config.Config) (Model, *engine.Engine, string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	e := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, configPath, filepath.Join(t.TempDir(), "m.json"))
	m := New(e)
	// Simulate the startup Refresh having landed: config is reachable only when
	// no command is in flight (skillmux-3vj), so clear the in-flight flag New sets.
	m.refreshing = false
	return m, e, configPath
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

	m, _ = step(t, m, runes("cc"))       // type name
	m, _ = step(t, m, key(tea.KeyTab))   // -> path field
	m, _ = step(t, m, runes("/tmp/skl")) // type path
	m, _ = step(t, m, key(tea.KeyEnter)) // submit

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

func TestConfigListsSourcesBeforeTargets(t *testing.T) {
	m, _, _ := newConfigModel(t, &config.Config{
		Targets: []config.TargetEntry{{Name: "t1", Path: "/t"}},
		Sources: []config.SourceEntry{{Name: "s1", Location: "/s"}},
	})
	entries := m.cfgEntries()
	if len(entries) != 2 || entries[0].kind != entrySource || entries[1].kind != entryTarget {
		t.Fatalf("sources should come before targets: %+v", entries)
	}
}

func TestConfigEditTargetFlow(t *testing.T) {
	m, e, configPath := newConfigModel(t, &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: "/old"}},
	})
	m, _ = step(t, m, runes("c")) // open config; cursor 0 -> target "cc"
	m, _ = step(t, m, runes("e")) // edit it
	if m.mode != modeForm || m.form == nil || !m.form.editing || m.form.origName != "cc" {
		t.Fatalf("e should open a prefilled edit form: mode=%v form=%+v", m.mode, m.form)
	}
	if got := m.form.inputs[0].Value(); got != "cc" {
		t.Fatalf("name field not prefilled, got %q", got)
	}

	m, _ = step(t, m, key(tea.KeyTab)) // -> path field
	m, _ = step(t, m, runes("2"))      // append to the prefilled "/old"
	m, _ = step(t, m, key(tea.KeyEnter))

	if m.mode != modeConfig {
		t.Fatalf("after save expected modeConfig, got %v", m.mode)
	}
	// Edited in place: still one target, not a second appended one.
	if len(e.Config.Targets) != 1 || e.Config.Targets[0].Path != "/old2" {
		t.Fatalf("edit not applied in place: %+v", e.Config.Targets)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Targets) != 1 || reloaded.Targets[0].Path != "/old2" {
		t.Errorf("edit not persisted: %+v", reloaded.Targets)
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

func TestConfigClearSourceCache(t *testing.T) {
	m, e, _ := newConfigModel(t, &config.Config{
		Sources: []config.SourceEntry{{Name: "remote", Location: "https://github.com/o/r"}},
	})
	// Stand in a cached copy on disk for the source.
	src := e.Config.DomainSources()[0]
	dir := e.Fetcher.CacheDirFor(src)
	if dir == "" {
		t.Fatal("expected github source to be cacheable")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	m, _ = step(t, m, runes("c")) // open config; cursor 0 -> source "remote"
	m, _ = step(t, m, runes("C")) // clear its cache

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cache dir not removed: stat err = %v", err)
	}
	if m.cfgMsg == "" {
		t.Error("expected a status message after clearing cache")
	}
}

// fakeToolHome points $HOME at a temp dir holding the given tool root dirs, so
// detection sees a controlled machine instead of the developer's real home.
func fakeToolHome(t *testing.T, roots ...string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, r := range roots {
		if err := os.MkdirAll(filepath.Join(home, r), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConfigShowsDetectedTools(t *testing.T) {
	fakeToolHome(t, ".claude")
	m, _, _ := newConfigModel(t, &config.Config{})
	m.width, m.height = 100, 24

	m, _ = step(t, m, runes("c")) // open config
	entries := m.cfgEntries()
	if len(entries) != 1 || entries[0].kind != entryDetected || entries[0].name != "claude-code" {
		t.Fatalf("expected one detected entry for claude-code, got %+v", entries)
	}
	if !strings.Contains(m.View(), "claude-code") {
		t.Errorf("config view should list the detected tool; got:\n%s", m.View())
	}
}

func TestConfigAddDetectedTarget(t *testing.T) {
	fakeToolHome(t, ".claude")
	m, e, configPath := newConfigModel(t, &config.Config{})

	m, _ = step(t, m, runes("c")) // open config; cursor 0 -> detected claude-code
	m, _ = step(t, m, runes("a")) // adopt it

	if len(e.Config.Targets) != 1 || e.Config.Targets[0].Name != "claude-code" ||
		e.Config.Targets[0].Path != "~/.claude/skills" {
		t.Fatalf("detected target not added: %+v", e.Config.Targets)
	}
	reloaded, _ := config.Load(configPath)
	if len(reloaded.Targets) != 1 {
		t.Errorf("detected target not persisted: %+v", reloaded.Targets)
	}
	// Adopted, so it must leave the detected section.
	if len(m.cfgDetected) != 0 {
		t.Errorf("candidate should disappear once adopted: %+v", m.cfgDetected)
	}
}

func TestConfigDeleteTargetRestoresCandidate(t *testing.T) {
	fakeToolHome(t, ".claude")
	m, e, _ := newConfigModel(t, &config.Config{
		Targets: []config.TargetEntry{{Name: "claude-code", Path: "~/.claude/skills"}},
	})

	m, _ = step(t, m, runes("c")) // open config; no candidates (tool is configured)
	if len(m.cfgDetected) != 0 {
		t.Fatalf("configured tool should not be a candidate: %+v", m.cfgDetected)
	}
	m, _ = step(t, m, runes("d")) // delete the target
	if len(e.Config.Targets) != 0 {
		t.Fatalf("target not deleted: %+v", e.Config.Targets)
	}
	if len(m.cfgDetected) != 1 || m.cfgDetected[0].Name != "claude-code" {
		t.Errorf("deleting the target should restore the candidate: %+v", m.cfgDetected)
	}
}

func TestConfigEditDetectedOpensPrefilledAddForm(t *testing.T) {
	fakeToolHome(t, ".claude")
	m, e, _ := newConfigModel(t, &config.Config{})

	m, _ = step(t, m, runes("c")) // open config; cursor 0 -> detected claude-code
	m, _ = step(t, m, runes("e")) // tweak before adopting
	if m.mode != modeForm || m.form == nil || m.form.editing {
		t.Fatalf("e on a detected row should open a prefilled ADD form: mode=%v form=%+v", m.mode, m.form)
	}
	if m.form.inputs[0].Value() != "claude-code" || m.form.inputs[1].Value() != "~/.claude/skills" {
		t.Fatalf("form not prefilled with the proposal: %q %q",
			m.form.inputs[0].Value(), m.form.inputs[1].Value())
	}

	m, _ = step(t, m, key(tea.KeyEnter)) // accept as-is
	if len(e.Config.Targets) != 1 || e.Config.Targets[0].Name != "claude-code" {
		t.Fatalf("prefilled form should add the target: %+v", e.Config.Targets)
	}
	if len(m.cfgDetected) != 0 {
		t.Errorf("candidate should disappear once adopted via form: %+v", m.cfgDetected)
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
