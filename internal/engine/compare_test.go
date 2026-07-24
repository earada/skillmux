package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/diff"
)

// skillIn returns the named skill from a catalog.
func skillIn(t *testing.T, cat Catalog, name string) AvailableSkill {
	t.Helper()
	for _, sk := range cat.Skills {
		if sk.Name == name {
			return sk
		}
	}
	t.Fatalf("skill %q not in catalog %+v", name, cat.Skills)
	return AvailableSkill{}
}

func TestCompareUpstreamDriftOnPristineCopy(t *testing.T) {
	e, _, skillDir, _ := newEnv(t)
	installDeploy(t, e)
	// Upstream moves on: an edited SKILL.md plus a brand-new file.
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v2")
	if err := os.WriteFile(filepath.Join(skillDir, "run.sh"), []byte("echo go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := e.Refresh()

	c, err := e.Compare(skillIn(t, cat, "deploy"), "cc")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Tracked || !c.Pristine {
		t.Errorf("an untouched installed copy should be tracked and pristine: %+v", c)
	}
	added, removed, modified := c.Summary.Counts()
	if added != 1 || removed != 0 || modified != 1 {
		t.Fatalf("counts = %d added / %d removed / %d modified: %+v", added, removed, modified, c.Summary.Changes)
	}
	// The diff runs installed-copy → source, so the new upstream line reads as an
	// addition (not a deletion, which would mean the arrow is backwards).
	var sawAdd bool
	for _, ch := range c.Summary.Changes {
		if ch.Path != "SKILL.md" {
			continue
		}
		for _, h := range ch.Hunks {
			for _, l := range h.Lines {
				if l.Kind == diff.Add && l.Text == "v2" {
					sawAdd = true
				}
			}
		}
	}
	if !sawAdd {
		t.Errorf("expected the new upstream line as an addition: %+v", c.Summary.Changes)
	}
}

func TestCompareFlagsHandEditedCopyAsNotPristine(t *testing.T) {
	e, targetPath, skillDir, _ := newEnv(t)
	installDeploy(t, e)
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v2")
	if err := os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := e.Refresh()

	c, err := e.Compare(skillIn(t, cat, "deploy"), "cc")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Tracked || c.Pristine {
		t.Fatalf("a hand-edited copy must be tracked but not pristine: %+v", c)
	}
	if c.Summary.Empty() {
		t.Fatal("expected the hand edit to show up as a difference")
	}
}

func TestCompareUntrackedFolderIsNotTracked(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	cat := e.Refresh()
	// A folder placed by hand at the destination — nothing in the Manifest.
	dir := filepath.Join(targetPath, "deploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := e.Compare(skillIn(t, cat, "deploy"), "cc")
	if err != nil {
		t.Fatal(err)
	}
	if c.Tracked || c.Pristine {
		t.Fatalf("an untracked folder must report Tracked=false: %+v", c)
	}
	if c.Summary.Empty() {
		t.Fatal("expected the untracked folder's content to differ from the source")
	}
}

func TestCompareUpToDateInstallationIsEmpty(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cat := installDeploy(t, e)
	c, err := e.Compare(skillIn(t, cat, "deploy"), "cc")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Summary.Empty() {
		t.Fatalf("a fresh install should have nothing to show: %+v", c.Summary.Changes)
	}
}

func TestCompareErrors(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	cat := e.Refresh()
	sk := skillIn(t, cat, "deploy")

	if _, err := e.Compare(sk, "nope"); err == nil || !strings.Contains(err.Error(), "unknown target") {
		t.Errorf("unknown target should error, got %v", err)
	}
	if _, err := e.Compare(sk, "cc"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("missing installed copy should error, got %v", err)
	}
	// A Skill removed upstream (no folder to compare against) even though a copy
	// is still installed.
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone := AvailableSkill{Name: "deploy", Source: "local", Unavailable: true}
	if _, err := e.Compare(gone, "cc"); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("unavailable skill should error, got %v", err)
	}
}

func TestInstalledCopy(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	if _, ok := e.InstalledCopy("cc", "deploy"); ok {
		t.Error("nothing installed yet, InstalledCopy should report false")
	}
	if _, ok := e.InstalledCopy("nope", "deploy"); ok {
		t.Error("unknown target should report false")
	}
	installDeploy(t, e)
	dir, ok := e.InstalledCopy("cc", "deploy")
	if !ok || dir != filepath.Join(targetPath, "deploy") {
		t.Errorf("InstalledCopy = %q, %v", dir, ok)
	}
}
