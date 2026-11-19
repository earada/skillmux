package apply

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// TestInstallCreatesMissingTargetDir reproduces skillmux-uog: a freshly
// adopted tool whose skills folder (e.g. ~/.codex/skills) was never created
// must still accept an install — the Target directory is created on demand.
func TestInstallCreatesMissingTargetDir(t *testing.T) {
	src := makeSource(t, "v1")
	targetPath := filepath.Join(t.TempDir(), "home", ".codex", "skills")
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
	if !ok || in.Fingerprint != "fp1" || in.Path != targetPath {
		t.Errorf("manifest entry wrong: %+v ok=%v", in, ok)
	}
	assertNoStagingLeftovers(t, targetPath)
}

// TestFailedInstallIntoMissingTargetLeavesNoStaging checks that when the
// Target directory had to be created and the copy then fails, no staging
// folder is left behind and nothing is recorded.
func TestFailedInstallIntoMissingTargetLeavesNoStaging(t *testing.T) {
	src := makeSource(t, "v1")
	targetPath := filepath.Join(t.TempDir(), "home", ".codex", "skills")
	man := &manifest.Manifest{}

	restore := copyTree
	copyTree = func(_, _ string) error { return errors.New("boom: read failed mid-copy") }
	defer func() { copyTree = restore }()

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp1"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatalf("expected failure, got %+v", rep.Results)
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Errorf("failed install must not be recorded in the manifest")
	}
	if exists(filepath.Join(targetPath, "deploy")) {
		t.Errorf("failed install must not leave a destination folder")
	}
	assertNoStagingLeftovers(t, targetPath)
}
