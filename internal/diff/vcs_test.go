package diff

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCompareIgnoresVCSMetadata covers skillmux-ia0: a `.git` directory or
// gitfile is not Skill content, so it must not show up as a change.
func TestCompareIgnoresVCSMetadata(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write := func(dir, rel, content string) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(oldDir, "SKILL.md", "same\n")
	write(newDir, "SKILL.md", "same\n")
	write(newDir, ".git/HEAD", "ref: refs/heads/main\n")
	write(newDir, "sub/.git", "gitdir: elsewhere\n")

	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Empty() {
		t.Fatalf("expected no changes, got %+v", s.Changes)
	}
}
