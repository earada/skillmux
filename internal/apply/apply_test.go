package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// makeSource creates a cached skill folder with one file and returns its dir.
func makeSource(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readInstalled(t *testing.T, targetPath, skill string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(targetPath, skill, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading installed skill: %v", err)
	}
	return string(b)
}

func TestInstallCopiesAndRecords(t *testing.T) {
	src := makeSource(t, "v1")
	targetPath := t.TempDir()
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp1"}},
		man, Options{},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "v1" {
		t.Errorf("installed content = %q, want v1", got)
	}
	in, ok := man.Find("t", "deploy")
	if !ok || in.Fingerprint != "fp1" || in.SourceName != "s" {
		t.Errorf("manifest entry wrong: %+v ok=%v", in, ok)
	}
}

func TestReinstallOverwritesTrackedFolder(t *testing.T) {
	src := makeSource(t, "v2")
	targetPath := t.TempDir()
	// Pre-existing tracked install with old content.
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("v1"), 0o644)
	os.WriteFile(filepath.Join(targetPath, "deploy", "stale.txt"), []byte("x"), 0o644)
	man := &manifest.Manifest{}
	man.Put(installation("deploy", "t", "s", "old"))

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Reinstall, SkillName: "deploy", SourceName: "s", TargetName: "t", Reason: reconcile.ReasonUpdateAvailable},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp2"}},
		man, Options{},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "v2" {
		t.Errorf("content = %q, want v2", got)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy", "stale.txt")); !os.IsNotExist(err) {
		t.Error("stale file from previous install should be gone after reinstall")
	}
	if in, _ := man.Find("t", "deploy"); in.Fingerprint != "fp2" {
		t.Errorf("fingerprint not updated: %+v", in)
	}
}

func TestUninstallRemovesFolderAndEntry(t *testing.T) {
	targetPath := t.TempDir()
	os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755)
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("v1"), 0o644)
	man := &manifest.Manifest{}
	man.Put(installation("deploy", "t", "s", "fp"))

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Uninstall, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		nil, man, Options{},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Error("folder should be removed")
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Error("manifest entry should be removed")
	}
}

func TestInstallRefusesUntrackedFolderByDefault(t *testing.T) {
	src := makeSource(t, "new")
	targetPath := t.TempDir()
	// An untracked folder placed by hand.
	os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755)
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("handmade"), 0o644)
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected refusal to overwrite untracked folder")
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "handmade" {
		t.Errorf("untracked folder was modified: %q", got)
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Error("nothing should be recorded for a refused install")
	}
}

func TestInstallOverwritesUntrackedWhenConfirmed(t *testing.T) {
	src := makeSource(t, "new")
	targetPath := t.TempDir()
	os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755)
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("handmade"), 0o644)
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp"}},
		man, Options{ConfirmOverwrite: func(_, _, _ string) bool { return true }},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success with confirm, got %+v", rep.Results)
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "new" {
		t.Errorf("content = %q, want new", got)
	}
}

func TestBestEffortContinuesAfterFailure(t *testing.T) {
	src := makeSource(t, "ok")
	targetPath := t.TempDir()
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			// Fails: no resolved skill provided for "missing".
			{Kind: reconcile.Install, SkillName: "missing", SourceName: "s", TargetName: "t"},
			// Succeeds.
			{Kind: reconcile.Install, SkillName: "good", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "good"}: {Dir: src, Fingerprint: "fp"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected one failure")
	}
	if _, ok := man.Find("t", "good"); !ok {
		t.Error("the good install should have proceeded despite the earlier failure")
	}
}

func installation(skill, target, source, fp string) domain.Installation {
	return domain.Installation{SkillName: skill, TargetName: target, SourceName: source, Fingerprint: fp}
}
