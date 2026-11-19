package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/fingerprint"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// uninstallModifiedEnv stands up a tracked installation whose on-disk copy was
// then edited by hand, plus the uninstall op that would delete it
// (skillmux-9nn).
func uninstallModifiedEnv(t *testing.T) (targetPath string, man *manifest.Manifest, plan reconcile.Plan) {
	t.Helper()
	targetPath = t.TempDir()
	dest := filepath.Join(targetPath, "deploy")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp, err := fingerprint.Dir(dest)
	if err != nil {
		t.Fatal(err)
	}
	man = &manifest.Manifest{}
	in := installation("deploy", "t", "s", fp)
	in.Path = targetPath
	man.Put(in)
	// The hand edit after install: recorded fingerprint no longer matches.
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("v1 + my local tweaks"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan = reconcile.Plan{Operations: []reconcile.Operation{
		{Kind: reconcile.Uninstall, SkillName: "deploy", SourceName: "s", TargetName: "t"},
	}}
	return targetPath, man, plan
}

func TestModifiedOverwritesDetectsModifiedUninstall(t *testing.T) {
	targetPath, man, plan := uninstallModifiedEnv(t)
	cols := ModifiedOverwrites(plan, map[string]string{"t": targetPath}, man)
	if len(cols) != 1 || cols[0].SkillName != "deploy" || cols[0].TargetName != "t" {
		t.Fatalf("expected one modified uninstall, got %+v", cols)
	}
}

func TestUninstallRefusesModifiedCopyWithoutConfirmation(t *testing.T) {
	targetPath, man, plan := uninstallModifiedEnv(t)
	rep := Apply(plan, map[string]string{"t": targetPath}, nil, man, Options{})
	if rep.AllOK() {
		t.Fatal("uninstalling a modified copy must fail without confirmation")
	}
	if !strings.Contains(rep.Results[0].Err.Error(), "locally modified") {
		t.Errorf("error should say why: %v", rep.Results[0].Err)
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "v1 + my local tweaks" {
		t.Errorf("local edits were destroyed: %q", got)
	}
	if _, ok := man.Find("t", "deploy"); !ok {
		t.Error("manifest must keep the refused uninstall's installation")
	}
}

func TestUninstallRemovesModifiedCopyWhenConfirmed(t *testing.T) {
	targetPath, man, plan := uninstallModifiedEnv(t)
	var asked bool
	rep := Apply(plan, map[string]string{"t": targetPath}, nil, man, Options{
		ConfirmModified: func(target, skill, dir string) bool { asked = true; return true },
	})
	if !rep.AllOK() {
		t.Fatalf("confirmed uninstall should succeed: %+v", rep.Results)
	}
	if !asked {
		t.Error("ConfirmModified should have been consulted")
	}
	if exists(filepath.Join(targetPath, "deploy")) {
		t.Error("folder should be removed")
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Error("manifest entry should be removed")
	}
}

func TestUninstallPristineCopyNeedsNoConfirmation(t *testing.T) {
	targetPath, man, plan := uninstallModifiedEnv(t)
	// Restore the copy to exactly what the manifest recorded.
	if err := os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cols := ModifiedOverwrites(plan, map[string]string{"t": targetPath}, man); len(cols) != 0 {
		t.Fatalf("pristine copy must not be flagged: %+v", cols)
	}
	rep := Apply(plan, map[string]string{"t": targetPath}, nil, man, Options{})
	if !rep.AllOK() {
		t.Fatalf("pristine uninstall should succeed: %+v", rep.Results)
	}
	if exists(filepath.Join(targetPath, "deploy")) {
		t.Error("folder should be removed")
	}
}

func TestUninstallMissingCopyNeedsNoConfirmation(t *testing.T) {
	targetPath, man, plan := uninstallModifiedEnv(t)
	if err := os.RemoveAll(filepath.Join(targetPath, "deploy")); err != nil {
		t.Fatal(err)
	}
	if cols := ModifiedOverwrites(plan, map[string]string{"t": targetPath}, man); len(cols) != 0 {
		t.Fatalf("missing copy must not be flagged: %+v", cols)
	}
	rep := Apply(plan, map[string]string{"t": targetPath}, nil, man, Options{})
	if !rep.AllOK() {
		t.Fatalf("missing-copy uninstall should succeed: %+v", rep.Results)
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Error("manifest entry should be removed")
	}
}
