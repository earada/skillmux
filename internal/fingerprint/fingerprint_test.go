package fingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildSkill creates a small skill tree under a fresh dir and returns it.
func buildSkill(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "SKILL.md"), "---\nname: deploy\n---\nbody")
	write(t, filepath.Join(dir, "scripts", "run.sh"), "echo hi")
	return dir
}

func TestSameContentSameFingerprint(t *testing.T) {
	a := buildSkill(t)
	b := buildSkill(t)
	fa, err := Dir(a)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := Dir(b)
	if err != nil {
		t.Fatal(err)
	}
	if fa != fb {
		t.Errorf("identical trees produced different fingerprints: %s vs %s", fa, fb)
	}
	if fa == "" {
		t.Error("fingerprint is empty")
	}
}

func TestContentChangeChangesFingerprint(t *testing.T) {
	dir := buildSkill(t)
	before, _ := Dir(dir)
	write(t, filepath.Join(dir, "scripts", "run.sh"), "echo bye")
	after, _ := Dir(dir)
	if before == after {
		t.Error("changing file content did not change fingerprint")
	}
}

func TestRenameChangesFingerprint(t *testing.T) {
	dir := buildSkill(t)
	before, _ := Dir(dir)
	if err := os.Rename(filepath.Join(dir, "scripts", "run.sh"), filepath.Join(dir, "scripts", "go.sh")); err != nil {
		t.Fatal(err)
	}
	after, _ := Dir(dir)
	if before == after {
		t.Error("renaming a file did not change fingerprint (path must be part of the hash)")
	}
}

func TestAddingFileChangesFingerprint(t *testing.T) {
	dir := buildSkill(t)
	before, _ := Dir(dir)
	write(t, filepath.Join(dir, "extra.txt"), "x")
	after, _ := Dir(dir)
	if before == after {
		t.Error("adding a file did not change fingerprint")
	}
}

func TestMissingDirIsError(t *testing.T) {
	if _, err := Dir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected error for missing directory")
	}
}
