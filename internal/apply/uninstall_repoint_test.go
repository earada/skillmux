package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/fingerprint"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

func writeSkill(t *testing.T, base, skill, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(base, skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, skill, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pristineFP is the real fingerprint of an installed copy, so the uninstall is
// not held back as "modified locally" (skillmux-9nn).
func pristineFP(t *testing.T, base, skill string) string {
	t.Helper()
	fp, err := fingerprint.Dir(filepath.Join(base, skill))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func uninstallPlan(skill, target string) reconcile.Plan {
	return reconcile.Plan{Operations: []reconcile.Operation{
		{Kind: reconcile.Uninstall, SkillName: skill, SourceName: "s", TargetName: target},
	}}
}

// skillmux-xkw: after a Target is re-pointed, uninstall must remove the copy at
// the Installation's recorded Path, never a same-named (untracked) folder at
// the Target's new path.
func TestUninstallAfterTargetRepointRemovesRecordedPathOnly(t *testing.T) {
	oldPath := t.TempDir()
	newPath := t.TempDir()
	writeSkill(t, oldPath, "deploy", "installed")
	writeSkill(t, newPath, "deploy", "hand-placed")

	man := &manifest.Manifest{}
	man.Put(domain.Installation{SkillName: "deploy", TargetName: "t", SourceName: "s", Path: oldPath, Fingerprint: pristineFP(t, oldPath, "deploy")})

	rep := Apply(uninstallPlan("deploy", "t"), map[string]string{"t": newPath}, nil, man, Options{})

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(oldPath, "deploy")); !os.IsNotExist(err) {
		t.Error("installed copy at the recorded path should be removed")
	}
	got, err := os.ReadFile(filepath.Join(newPath, "deploy", "SKILL.md"))
	if err != nil || string(got) != "hand-placed" {
		t.Errorf("untracked folder at the new path must be untouched, got %q, err %v", got, err)
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Error("manifest entry should be removed")
	}
}

// With a recorded Path the Installation is self-locating, so uninstall works
// even if the Target is no longer configured.
func TestUninstallWithRecordedPathDoesNotNeedConfiguredTarget(t *testing.T) {
	oldPath := t.TempDir()
	writeSkill(t, oldPath, "deploy", "installed")

	man := &manifest.Manifest{}
	man.Put(domain.Installation{SkillName: "deploy", TargetName: "gone", SourceName: "s", Path: oldPath, Fingerprint: pristineFP(t, oldPath, "deploy")})

	rep := Apply(uninstallPlan("deploy", "gone"), map[string]string{}, nil, man, Options{})

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(oldPath, "deploy")); !os.IsNotExist(err) {
		t.Error("installed copy at the recorded path should be removed")
	}
	if _, ok := man.Find("gone", "deploy"); ok {
		t.Error("manifest entry should be removed")
	}
}

// A legacy Installation with no recorded Path falls back to the Target's
// current path.
func TestUninstallLegacyEmptyPathFallsBackToTargetPath(t *testing.T) {
	targetPath := t.TempDir()
	writeSkill(t, targetPath, "deploy", "installed")

	man := &manifest.Manifest{}
	man.Put(domain.Installation{SkillName: "deploy", TargetName: "t", SourceName: "s", Fingerprint: pristineFP(t, targetPath, "deploy")})

	rep := Apply(uninstallPlan("deploy", "t"), map[string]string{"t": targetPath}, nil, man, Options{})

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Error("folder should be removed")
	}
}

// The containment backstop still applies to the recorded Path: a malformed name
// may not escape it.
func TestUninstallRecordedPathRefusesNameThatEscapes(t *testing.T) {
	root := t.TempDir()
	recorded := filepath.Join(root, "target")
	writeSkill(t, root, "victim", "keep")
	if err := os.MkdirAll(recorded, 0o755); err != nil {
		t.Fatal(err)
	}

	man := &manifest.Manifest{}
	man.Put(domain.Installation{SkillName: "../victim", TargetName: "t", SourceName: "s", Path: recorded, Fingerprint: "fp"})

	rep := Apply(uninstallPlan("../victim", "t"), map[string]string{"t": t.TempDir()}, nil, man, Options{})

	if rep.AllOK() {
		t.Fatal("expected refusal for escaping name")
	}
	if _, err := os.Stat(filepath.Join(root, "victim", "SKILL.md")); err != nil {
		t.Errorf("folder outside the recorded path must be untouched: %v", err)
	}
	if _, ok := man.Find("t", "../victim"); !ok {
		t.Error("manifest entry should be kept on refusal")
	}
}
