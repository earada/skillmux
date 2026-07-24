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

// modifiedEnv stands up a tracked installation whose on-disk copy was then
// edited by hand, plus the reinstall op that would overwrite it.
func modifiedEnv(t *testing.T) (targetPath string, man *manifest.Manifest, plan reconcile.Plan, resolved map[SkillID]ResolvedSkill) {
	t.Helper()
	src := makeSource(t, "v2")
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
	man.Put(installation("deploy", "t", "s", fp))
	// The hand edit after install: recorded fingerprint no longer matches.
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("v1 + my local tweaks"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan = reconcile.Plan{Operations: []reconcile.Operation{
		{Kind: reconcile.Reinstall, SkillName: "deploy", SourceName: "s", TargetName: "t", Reason: reconcile.ReasonUpdateAvailable},
	}}
	resolved = map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp2"}}
	return targetPath, man, plan, resolved
}

func TestModifiedOverwritesDetectsHandEdit(t *testing.T) {
	targetPath, man, plan, _ := modifiedEnv(t)
	cols := ModifiedOverwrites(plan, map[string]string{"t": targetPath}, man)
	if len(cols) != 1 || cols[0].SkillName != "deploy" || cols[0].TargetName != "t" {
		t.Fatalf("expected one modified overwrite, got %+v", cols)
	}
}

func TestModifiedOverwritesIgnoresPristineCopy(t *testing.T) {
	targetPath, man, plan, _ := modifiedEnv(t)
	// Restore the copy to exactly what the manifest recorded.
	if err := os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cols := ModifiedOverwrites(plan, map[string]string{"t": targetPath}, man); len(cols) != 0 {
		t.Fatalf("pristine copy must not be a modified overwrite: %+v", cols)
	}
}

func TestModifiedOverwritesIgnoresMissingCopy(t *testing.T) {
	targetPath, man, plan, _ := modifiedEnv(t)
	// A hand-deleted copy has no local edits to lose; restoring it needs no
	// confirmation.
	if err := os.RemoveAll(filepath.Join(targetPath, "deploy")); err != nil {
		t.Fatal(err)
	}
	if cols := ModifiedOverwrites(plan, map[string]string{"t": targetPath}, man); len(cols) != 0 {
		t.Fatalf("missing copy must not be a modified overwrite: %+v", cols)
	}
}

func TestReinstallRefusesModifiedCopyWithoutConfirmation(t *testing.T) {
	targetPath, man, plan, resolved := modifiedEnv(t)
	rep := Apply(plan, map[string]string{"t": targetPath}, resolved, man, Options{})

	if rep.AllOK() {
		t.Fatal("reinstall over a modified copy must fail without confirmation")
	}
	if !strings.Contains(rep.Results[0].Err.Error(), "locally modified") {
		t.Errorf("error should say why: %v", rep.Results[0].Err)
	}
	// The local edits survive untouched.
	if got := readInstalled(t, targetPath, "deploy"); got != "v1 + my local tweaks" {
		t.Errorf("local edits were clobbered: %q", got)
	}
	if in, _ := man.Find("t", "deploy"); in.Fingerprint == "fp2" {
		t.Error("manifest must not record the refused reinstall")
	}
}

func TestReinstallOverwritesModifiedCopyWhenConfirmed(t *testing.T) {
	targetPath, man, plan, resolved := modifiedEnv(t)
	rep := Apply(plan, map[string]string{"t": targetPath}, resolved, man, Options{
		ConfirmModified: func(target, skill, dir string) bool { return true },
	})

	if !rep.AllOK() {
		t.Fatalf("confirmed reinstall should succeed: %+v", rep.Results)
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "v2" {
		t.Errorf("content = %q, want v2", got)
	}
	if in, _ := man.Find("t", "deploy"); in.Fingerprint != "fp2" {
		t.Errorf("fingerprint not updated: %+v", in)
	}
}
