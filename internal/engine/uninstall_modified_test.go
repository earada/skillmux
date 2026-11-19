package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/reconcile"
)

// An Uninstall of a hand-edited copy discards those edits just like a
// Reinstall would, so Preview must flag it for confirmation (skillmux-9nn).
func TestPreviewListsModifiedUninstall(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	installDeploy(t, e)
	if err := os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := e.Refresh()
	pre := e.Preview(nil, cat) // deselect everything: uninstall deploy
	if len(pre.Plan.Operations) != 1 || pre.Plan.Operations[0].Kind != reconcile.Uninstall {
		t.Fatalf("expected one uninstall, got %+v", pre.Plan.Operations)
	}
	if len(pre.Modified) != 1 || pre.Modified[0].SkillName != "deploy" {
		t.Fatalf("expected the uninstall flagged as modified, got %+v", pre.Modified)
	}
}

func TestPreviewNoModifiedForPristineUninstall(t *testing.T) {
	e, _, _, _ := newEnv(t)
	installDeploy(t, e)
	cat := e.Refresh()
	pre := e.Preview(nil, cat)
	if len(pre.Plan.Operations) != 1 || pre.Plan.Operations[0].Kind != reconcile.Uninstall {
		t.Fatalf("expected one uninstall, got %+v", pre.Plan.Operations)
	}
	if len(pre.Modified) != 0 {
		t.Fatalf("pristine copy must not be flagged: %+v", pre.Modified)
	}
}
