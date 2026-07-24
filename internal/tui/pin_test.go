package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// pinFixture builds a Model over one local Source holding "deploy", with a real
// Config path so 'P' can persist. When install is true the Skill is installed into
// target "cc" and the Source then drifts — the state a Pin exists for. The Config
// path is returned so a test can prove the Pin reached the file.
func pinFixture(t *testing.T, install bool) (Model, *engine.Engine, string) {
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

	if install {
		cat := e.Refresh()
		desired := []reconcile.Cell{{Skill: "deploy", Source: "local", Target: "cc"}}
		rep, err := e.Apply(e.Preview(desired, cat), apply.Options{})
		if err != nil || !rep.AllOK() {
			t.Fatalf("install failed: err=%v rep=%+v", err, rep)
		}
		mustWrite(t, filepath.Join(dir, "SKILL.md"), "---\nname: deploy\ndescription: d\n---\nv2")
	}

	m := New(e).onRefreshed(e.Refresh())
	m.width, m.height = 100, 24
	return m, e, configPath
}

func TestPinKeyTogglesAndPersists(t *testing.T) {
	m, e, configPath := pinFixture(t, true)
	if got := m.status[statusKey{"deploy", "local", "cc"}]; got != domain.StatusUpdateAvailable {
		t.Fatalf("fixture should start drifted and unpinned, status = %v", got)
	}

	m = applyKeys(m, runes("P"))
	if !e.IsPinned("deploy", "local") {
		t.Fatal("'P' should pin the row under the cursor")
	}
	if !strings.Contains(m.matrixMsg, "pinned") {
		t.Errorf("expected a confirmation note, got %q", m.matrixMsg)
	}
	// The row's status flips immediately, without waiting for a Refresh.
	if got := m.status[statusKey{"deploy", "local", "cc"}]; got != domain.StatusPinned {
		t.Errorf("status = %v, want pinned", got)
	}
	// And it reaches the file, not just memory.
	saved, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.IsPinned("deploy", "local") {
		t.Fatalf("pin was not persisted: %+v", saved.Pins)
	}

	m = applyKeys(m, runes("P"))
	if e.IsPinned("deploy", "local") {
		t.Fatal("a second 'P' should unpin")
	}
	if !strings.Contains(m.matrixMsg, "unpinned") {
		t.Errorf("expected an unpin note, got %q", m.matrixMsg)
	}
	if got := m.status[statusKey{"deploy", "local", "cc"}]; got != domain.StatusUpdateAvailable {
		t.Errorf("status = %v, want the update signal back", got)
	}
	saved, _ = config.Load(configPath)
	if len(saved.Pins) != 0 {
		t.Fatalf("unpin was not persisted: %+v", saved.Pins)
	}
}

func TestPinnedRowAndLegendAreMarked(t *testing.T) {
	m, _, _ := pinFixture(t, true)
	m = applyKeys(m, runes("P"))
	out := m.View()
	if !strings.Contains(out, pinnedGlyph) {
		t.Errorf("the matrix should show the pin glyph:\n%s", out)
	}
	if !strings.Contains(out, pinnedGlyph+" pinned") {
		t.Errorf("the legend should explain the glyph:\n%s", out)
	}
}

// A Pin on a Skill installed nowhere has no cell to colour, so the row label must
// carry the marker — otherwise pressing 'P' would look like nothing happened.
func TestPinnedRowMarkedWhenNotInstalled(t *testing.T) {
	m, _, _ := pinFixture(t, false)
	m = applyKeys(m, runes("P"))
	if got := m.status[statusKey{"deploy", "local", "cc"}]; got != domain.StatusNotInstalled {
		t.Fatalf("a pin must not fake an installation: status = %v", got)
	}
	if !strings.Contains(m.View(), pinnedGlyph) {
		t.Errorf("the pinned row should still be marked:\n%s", m.View())
	}
}

// A Skill-wide or Source-wide Pin can only be written by hand, so the per-row
// toggle must explain itself instead of silently doing nothing.
func TestPinKeyOnWidePinSteersToConfig(t *testing.T) {
	m, e, _ := pinFixture(t, true)
	e.Config.Pins = []config.PinEntry{{Source: "local"}}

	m = applyKeys(m, runes("P"))
	if len(e.Config.Pins) != 1 {
		t.Fatalf("a wide pin must not be narrowed by the toggle: %+v", e.Config.Pins)
	}
	if !strings.Contains(m.matrixMsg, "config.toml") {
		t.Errorf("expected to be steered to the TOML, got %q", m.matrixMsg)
	}
}

// The note is transient: it says what just happened, not what is true forever.
func TestPinNoteClearsOnNextKey(t *testing.T) {
	m, _, _ := pinFixture(t, true)
	m = applyKeys(m, runes("P"))
	if m.matrixMsg == "" {
		t.Fatal("expected a note after pinning")
	}
	m = applyKeys(m, runes("j"))
	if m.matrixMsg != "" {
		t.Errorf("the note should clear on the next keystroke, got %q", m.matrixMsg)
	}
}

func TestPinnedSkillViewAndDiffSayPinned(t *testing.T) {
	m, _, _ := pinFixture(t, true)
	m = applyKeys(m, runes("P"))

	// The skill explorer spells the status out…
	m = applyKeys(m, runes("v"))
	if !strings.Contains(m.View(), "pinned (frozen at the installed version)") {
		t.Errorf("skill view should explain the pinned status:\n%s", m.View())
	}
	// …and the diff says the changes are being held back, not pending.
	tm, cmd := m.Update(runes("d"))
	m = settle(t, tm.(Model), cmd)
	if !m.diffComp.Pinned || !strings.Contains(m.View(), "holding back") {
		t.Errorf("diff should flag the pin:\n%s", m.View())
	}
}

// A pinned row plans nothing, so the Plan stays empty even with upstream drift.
func TestPinnedRowPlansNothing(t *testing.T) {
	m, _, _ := pinFixture(t, true)
	m = applyKeys(m, runes("P"), runes("p"))
	if len(m.preview.Plan.Operations) != 0 {
		t.Fatalf("a pinned row must not be reinstalled: %+v", m.preview.Plan.Operations)
	}
	if !strings.Contains(m.View(), "Nothing to do") {
		t.Errorf("plan should be empty:\n%s", m.View())
	}
}
