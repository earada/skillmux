package tui

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
)

// stallRefresh starts a real Engine.Refresh on a background goroutine — as
// refreshCmd does — and holds it in flight (and so holding the Engine's opMu)
// by planting a SKILL.md that is a FIFO in srcRoot: the scanner's read blocks
// until a writer appears. It returns only once Refresh is provably blocked
// inside the scan. The returned release lets the Refresh finish; it is also
// registered with t.Cleanup so a failing test never leaks the goroutine.
func stallRefresh(t *testing.T, e *engine.Engine, srcRoot string) (release func()) {
	t.Helper()
	dir := filepath.Join(srcRoot, "stall")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "SKILL.md")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	done := make(chan struct{})
	go func() { e.Refresh(); close(done) }()

	// Opening the write end blocks until the scanner opens the read end, so
	// once this returns Refresh is parked mid-scan with opMu held.
	w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		// Swap in a regular file so any later read (fingerprinting) never
		// reopens the FIFO, then feed and close the read the scanner is in.
		content := "---\nname: stall\ndescription: d\n---\nv1"
		_ = os.Remove(fifo)
		mustWrite(t, fifo, content)
		_, _ = w.WriteString(content)
		_ = w.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("stalled Refresh did not finish after release")
		}
	}
	t.Cleanup(release)
	return release
}

// pinBusyFixture is pinFixture with the Source root exposed so a Refresh can
// be stalled on it.
func pinBusyFixture(t *testing.T) (Model, *engine.Engine, string, string) {
	t.Helper()
	srcRoot := t.TempDir()
	dir := filepath.Join(srcRoot, "deploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "SKILL.md"), "---\nname: deploy\ndescription: d\n---\nv1")
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	e := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()},
		configPath, filepath.Join(t.TempDir(), "m.json"))
	m := New(e).onRefreshed(e.Refresh())
	m.width, m.height = 100, 24
	return m, e, configPath, srcRoot
}

// pressWithin sends key to m on another goroutine and fails the test if the
// UI loop does not return within a short deadline — i.e. the key froze it.
func pressWithin(t *testing.T, m Model, key string) Model {
	t.Helper()
	out := make(chan Model, 1)
	go func() {
		upd, _ := m.Update(runes(key))
		out <- upd.(Model)
	}()
	select {
	case got := <-out:
		return got
	case <-time.After(2 * time.Second):
		t.Fatalf("pressing %q blocked the UI loop while a command was in flight", key)
		return m
	}
}

func TestPinKeyDoesNotBlockWhileBusy(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Model)
		want string
	}{
		{"refreshing", func(m *Model) { m.refreshing = true }, "refresh"},
		{"applying", func(m *Model) { m.applying = true }, "apply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, e, configPath, srcRoot := pinBusyFixture(t)
			release := stallRefresh(t, e, srcRoot)
			tc.set(&m)

			m = pressWithin(t, m, "P")
			release()

			if e.IsPinned("deploy", "local") {
				t.Error("'P' must not pin while a command is in flight")
			}
			if _, err := os.Stat(configPath); err == nil {
				saved, err := config.Load(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if len(saved.Pins) != 0 {
					t.Errorf("Config must be unchanged, got pins %+v", saved.Pins)
				}
			}
			if !strings.Contains(m.matrixMsg, "busy") || !strings.Contains(m.matrixMsg, tc.want) {
				t.Errorf("expected a busy note mentioning %q, got %q", tc.want, m.matrixMsg)
			}
		})
	}
}

func TestPinKeyStillTogglesWhenIdle(t *testing.T) {
	m, e, _, _ := pinBusyFixture(t)
	m = pressWithin(t, m, "P")
	if !e.IsPinned("deploy", "local") {
		t.Fatal("'P' should pin when idle")
	}
	if strings.Contains(m.matrixMsg, "busy") {
		t.Errorf("idle toggle should not report busy: %q", m.matrixMsg)
	}
}

// edgeBusyFixture opens the skill view on "a", whose only edge (to "b") is a
// Dependency, over an Engine with a real Config path and a stallable Source.
func edgeBusyFixture(t *testing.T) (Model, *engine.Engine, string) {
	t.Helper()
	srcRoot := t.TempDir()
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	e := engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()},
		configPath, filepath.Join(t.TempDir(), "m.json"))
	a := engine.AvailableSkill{Name: "a", Source: "local", Refs: []string{"b"}}
	b := engine.AvailableSkill{Name: "b", Source: "local"}
	m := New(e)
	m.refreshing = false
	m = injectCatalog(m, a, b)
	m.row = 0
	m = m.enterSkillView()
	if ed, ok := m.curEdge(); !ok || ed.suggestion {
		t.Fatalf("expected cursor on a Dependency edge, got %+v ok=%v", ed, ok)
	}
	return m, e, srcRoot
}

func TestEdgeToggleDoesNotBlockWhileBusy(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Model)
		want string
	}{
		{"refreshing", func(m *Model) { m.refreshing = true }, "refresh"},
		{"applying", func(m *Model) { m.applying = true }, "apply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, e, srcRoot := edgeBusyFixture(t)
			release := stallRefresh(t, e, srcRoot)
			tc.set(&m)

			m = pressWithin(t, m, "t")
			release()

			if e.Config.IsSuggestion("a", "b") {
				t.Error("'t' must not toggle the edge while a command is in flight")
			}
			if ed, _ := m.curEdge(); ed.suggestion {
				t.Error("the edge should still read as a Dependency")
			}
			if !strings.Contains(m.viewMsg, "busy") || !strings.Contains(m.viewMsg, tc.want) {
				t.Errorf("expected a busy note mentioning %q, got %q", tc.want, m.viewMsg)
			}
		})
	}
}

func TestEdgeToggleStillWorksWhenIdle(t *testing.T) {
	m, e, _ := edgeBusyFixture(t)
	m = pressWithin(t, m, "t")
	if !e.Config.IsSuggestion("a", "b") {
		t.Fatal("'t' should toggle the edge when idle")
	}
	if ed, _ := m.curEdge(); !ed.suggestion {
		t.Error("the edge should now read as a Suggestion")
	}
}
