package apply

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopyDirSkipsVCSMetadata covers skillmux-ia0: installing a Skill whose
// folder is a git clone root must not copy the `.git` directory (or a nested
// `.git` gitfile) into the Target.
func TestCopyDirSkipsVCSMetadata(t *testing.T) {
	src := t.TempDir()
	for rel, content := range map[string]string{
		"SKILL.md":     "body",
		".git/HEAD":    "ref: refs/heads/main\n",
		"sub/.git":     "gitdir: elsewhere\n",
		"sub/keep.txt": "keep",
	} {
		full := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(t.TempDir(), "skill")
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}
	for _, rel := range []string{"SKILL.md", "sub/keep.txt"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("%s not copied: %v", rel, err)
		}
	}
	for _, rel := range []string{".git", "sub/.git"} {
		if _, err := os.Lstat(filepath.Join(dst, rel)); !os.IsNotExist(err) {
			t.Errorf("%s copied into target (err=%v)", rel, err)
		}
	}
}
