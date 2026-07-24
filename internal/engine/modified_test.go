package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/domain"
)

// installDeploy installs the fixture skill into the fixture target and returns
// the fresh catalog.
func installDeploy(t *testing.T, e *Engine) Catalog {
	t.Helper()
	cat := e.Refresh()
	rep, err := e.Apply(e.Preview(cell(), cat), apply.Options{})
	if err != nil || !rep.AllOK() {
		t.Fatalf("install failed: err=%v rep=%+v", err, rep)
	}
	return cat
}

func TestStatusPristineCopyIsUpToDate(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cat := installDeploy(t, e)
	if got := statusOf(e, cat, "deploy", "cc"); got != domain.StatusUpToDate {
		t.Errorf("status = %v, want up-to-date", got)
	}
}

func TestStatusHandEditedCopyIsModified(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	cat := installDeploy(t, e)
	if err := os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(e, cat, "deploy", "cc"); got != domain.StatusModified {
		t.Errorf("status = %v, want modified-locally", got)
	}
}

func TestStatusModifiedWinsOverUpstreamDrift(t *testing.T) {
	e, targetPath, skillDir, _ := newEnv(t)
	installDeploy(t, e)
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v2") // upstream drift
	if err := os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := e.Refresh()
	// Both drifted: the local-edit warning must win — a reinstall would discard
	// the user's changes, which matters more than the pending update.
	if got := statusOf(e, cat, "deploy", "cc"); got != domain.StatusModified {
		t.Errorf("status = %v, want modified-locally", got)
	}
}

func TestStatusMissingCopyIsModified(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	cat := installDeploy(t, e)
	if err := os.RemoveAll(filepath.Join(targetPath, "deploy")); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(e, cat, "deploy", "cc"); got != domain.StatusModified {
		t.Errorf("status = %v, want modified-locally (reality diverged from manifest)", got)
	}
}

func TestPreviewListsModifiedOverwrites(t *testing.T) {
	e, targetPath, skillDir, _ := newEnv(t)
	installDeploy(t, e)
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v2") // reinstall will be planned
	if err := os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := e.Refresh()
	pre := e.Preview(cell(), cat)
	if len(pre.Plan.Operations) != 1 {
		t.Fatalf("expected one reinstall, got %+v", pre.Plan.Operations)
	}
	if len(pre.Modified) != 1 || pre.Modified[0].SkillName != "deploy" {
		t.Fatalf("expected the reinstall flagged as modified overwrite, got %+v", pre.Modified)
	}
}

func TestPreviewNoModifiedForPristineReinstall(t *testing.T) {
	e, _, skillDir, _ := newEnv(t)
	installDeploy(t, e)
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v2") // upstream drift only
	cat := e.Refresh()
	pre := e.Preview(cell(), cat)
	if len(pre.Plan.Operations) != 1 {
		t.Fatalf("expected one reinstall, got %+v", pre.Plan.Operations)
	}
	if len(pre.Modified) != 0 {
		t.Fatalf("pristine copy must not be flagged: %+v", pre.Modified)
	}
}
