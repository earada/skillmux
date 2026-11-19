package fingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

func writeVCSFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDirIgnoresVCSMetadata covers skillmux-ia0: a `.git` directory (a clone
// root that is itself a Skill) or `.git` gitfile (worktree/submodule) at any
// depth is VCS metadata, not Skill content, so it must not affect the
// fingerprint — otherwise a no-op `git fetch` reports "update available".
func TestDirIgnoresVCSMetadata(t *testing.T) {
	plain := t.TempDir()
	writeVCSFile(t, plain, "SKILL.md", "body")
	writeVCSFile(t, plain, "sub/x.txt", "x")

	withGit := t.TempDir()
	writeVCSFile(t, withGit, "SKILL.md", "body")
	writeVCSFile(t, withGit, "sub/x.txt", "x")
	writeVCSFile(t, withGit, ".git/HEAD", "ref: refs/heads/main\n")
	writeVCSFile(t, withGit, ".git/FETCH_HEAD", "abc\n")
	writeVCSFile(t, withGit, "sub/.git", "gitdir: ../../.git/modules/sub\n")

	a, err := Dir(plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Dir(withGit)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf(".git metadata changed the fingerprint: %s != %s", a, b)
	}
}
