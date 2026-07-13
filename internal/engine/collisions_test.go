package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreviewSurfacesUntrackedCollision(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	// An untracked folder placed by hand where "deploy" would install.
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	cat := e.Refresh()

	cols := e.Preview(cell(), cat).Collisions
	if len(cols) != 1 {
		t.Fatalf("expected 1 collision, got %+v", cols)
	}
	if cols[0].SkillName != "deploy" || cols[0].TargetName != "cc" || cols[0].Dir == "" {
		t.Errorf("collision metadata wrong: %+v", cols[0])
	}
}

func TestPreviewNoCollisionWhenDestFree(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cat := e.Refresh()
	if cols := e.Preview(cell(), cat).Collisions; len(cols) != 0 {
		t.Fatalf("expected no collisions, got %+v", cols)
	}
}
