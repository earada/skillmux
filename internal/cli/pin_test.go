package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/config"
)

// assertInstalledBody checks the installed copy's SKILL.md still carries body.
func assertInstalledBody(t *testing.T, targetPath, body string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), body) {
		t.Errorf("installed copy should still be %q, got %q", body, got)
	}
}

func TestStatusShowsPinned(t *testing.T) {
	e, _, _ := newEnv(t)
	install(t, e)
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}

	code, out, _ := run(e, "", "status")
	if code != exitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "pinned") || !strings.Contains(out, "1 pinned") {
		t.Errorf("status should surface the pin:\n%s", out)
	}
}

// The point of a Pin is that automation stops nagging: a frozen row is not a
// pending update, so `check` must stay clean and quiet.
func TestCheckIgnoresPinned(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2") // upstream drift
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}

	code, out, _ := run(e, "", "check")
	if code != exitOK {
		t.Fatalf("a pinned row must not read as pending, code=%d\n%s", code, out)
	}
	if strings.Contains(out, "update available") {
		t.Errorf("check should say nothing about a pinned row:\n%s", out)
	}
}

func TestApplyLeavesPinnedAlone(t *testing.T) {
	e, targetPath, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2")
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}

	code, out, _ := run(e, "", "apply", "--yes")
	if code != exitOK || !strings.Contains(out, "Nothing to do.") {
		t.Fatalf("apply should have nothing to do; code=%d out=%q", code, out)
	}
	assertInstalledBody(t, targetPath, "v1")
}

// `diff` follows `check`: a pinned row is out of the default walk (it would keep a
// CI gate red forever) but visible when named, flagged as held back.
func TestDiffSkipsPinnedUnlessNamed(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2")
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}}

	code, out, _ := run(e, "", "diff")
	if code != exitOK || strings.Contains(out, "@@") {
		t.Fatalf("the default walk should skip a pinned row; code=%d out=%q", code, out)
	}

	code, out, _ = run(e, "", "diff", "deploy")
	if code != exitPending || !strings.Contains(out, "@@") {
		t.Fatalf("naming the row should still diff it; code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "! pinned") {
		t.Errorf("the diff should flag the pin:\n%s", out)
	}
}
