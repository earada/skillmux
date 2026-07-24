package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
)

// A pinned row reports Pinned even while everything is current, so the Pin stays
// visible in the matrix instead of only appearing once upstream moves.
func TestStatusPinnedWhenCurrent(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cat := installDeploy(t, e)
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}
	if got := statusOf(e, cat, "deploy", "cc"); got != domain.StatusPinned {
		t.Errorf("status = %v, want pinned", got)
	}
}

// The point of the Pin: upstream drift is not reported as an available update.
func TestStatusPinnedHidesUpstreamDrift(t *testing.T) {
	e, _, skillDir, _ := newEnv(t)
	installDeploy(t, e)
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v2")
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}

	cat := e.Refresh()
	if got := statusOf(e, cat, "deploy", "cc"); got != domain.StatusPinned {
		t.Errorf("status = %v, want pinned", got)
	}
}

// Local drift is a statement about reality diverging from the Manifest, which a
// Pin does not make untrue — so it still wins.
func TestStatusModifiedWinsOverPinned(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	cat := installDeploy(t, e)
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}
	if err := os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(e, cat, "deploy", "cc"); got != domain.StatusModified {
		t.Errorf("status = %v, want modified-locally", got)
	}
}

// A Pin on a Skill that is not installed is inert: it freezes an existing copy, it
// does not forbid a first install.
func TestStatusPinnedNotInstalledStaysNotInstalled(t *testing.T) {
	e, _, _, _ := newEnv(t)
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}
	cat := e.Refresh()
	if got := statusOf(e, cat, "deploy", "cc"); got != domain.StatusNotInstalled {
		t.Errorf("status = %v, want not-installed", got)
	}
}

func TestPreviewSkipsPinnedReinstall(t *testing.T) {
	e, _, skillDir, _ := newEnv(t)
	installDeploy(t, e)
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v2")
	cat := e.Refresh()

	// Unpinned, the drift plans a reinstall…
	if pre := e.Preview(cell(), cat); len(pre.Plan.Operations) != 1 {
		t.Fatalf("expected one reinstall while unpinned, got %+v", pre.Plan.Operations)
	}
	// …and pinned, it plans nothing at all.
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}
	if pre := e.Preview(cell(), cat); len(pre.Plan.Operations) != 0 {
		t.Fatalf("a pinned row must not be reinstalled, got %+v", pre.Plan.Operations)
	}
}

func TestTogglePinPersistsAndReports(t *testing.T) {
	e, _, _, _ := newEnv(t)
	now, err := e.TogglePin("deploy", "local")
	if err != nil || !now {
		t.Fatalf("first toggle should pin: now=%v err=%v", now, err)
	}
	if !e.IsPinned("deploy", "local") {
		t.Error("engine should report the row as pinned")
	}
	// The Config file is the source of truth: reload it from disk.
	reloaded, err := config.Load(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.IsPinned("deploy", "local") {
		t.Fatalf("pin was not persisted: %+v", reloaded.Pins)
	}

	now, err = e.TogglePin("deploy", "local")
	if err != nil || now {
		t.Fatalf("second toggle should unpin: now=%v err=%v", now, err)
	}
	reloaded, _ = config.Load(e.configPath)
	if len(reloaded.Pins) != 0 {
		t.Fatalf("unpin was not persisted: %+v", reloaded.Pins)
	}
}

func TestTogglePinWithoutConfigPath(t *testing.T) {
	e, _, _, _ := newEnv(t)
	e.configPath = ""
	if _, err := e.TogglePin("deploy", "local"); err == nil {
		t.Fatal("toggling without a config path should error rather than lose the pin")
	}
}

func TestHasWidePinSteersToTOML(t *testing.T) {
	e, _, _, _ := newEnv(t)
	e.Config.Pins = []config.PinEntry{{Source: "local"}} // Source-wide, hand-written
	if !e.HasWidePin("deploy", "local") {
		t.Error("a Source-wide pin should be reported as wide")
	}
	if !e.IsPinned("deploy", "local") {
		t.Error("a Source-wide pin should still freeze the row")
	}
}

func TestComparePinnedFlag(t *testing.T) {
	e, _, skillDir, _ := newEnv(t)
	installDeploy(t, e)
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v2")
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}
	cat := e.Refresh()

	c, err := e.Compare(skillIn(t, cat, "deploy"), "cc")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Pinned {
		t.Error("a pinned row's comparison should say so")
	}
	if c.Summary.Empty() {
		t.Error("the pin holds back real changes; they should still be visible")
	}
}
