package source

import (
	"os"
	"path/filepath"
	"testing"
)

func writeVCS(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScanDoesNotDescendIntoGitDir covers skillmux-ia0: anything under `.git`
// is VCS metadata, so a SKILL.md there must not be catalogued.
func TestScanDoesNotDescendIntoGitDir(t *testing.T) {
	root := t.TempDir()
	writeVCS(t, root, "real/SKILL.md", "---\nname: real\ndescription: d\n---\n")
	writeVCS(t, root, ".git/hidden/SKILL.md", "---\nname: hidden\ndescription: d\n---\n")

	skills, err := Scan(root, "src")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "real" {
		t.Fatalf("want only the real skill, got %+v", skills)
	}
}

// TestReferencesIgnoresVCSMetadata covers skillmux-ia0: text inside `.git`
// (e.g. commit messages, a gitfile) must not create Skill references.
func TestReferencesIgnoresVCSMetadata(t *testing.T) {
	dir := t.TempDir()
	writeVCS(t, dir, "SKILL.md", "no refs here\n")
	writeVCS(t, dir, ".git/COMMIT_EDITMSG", "see /other\n")
	writeVCS(t, dir, "sub/.git", "use /other\n")

	refs, err := References(dir, map[string]bool{"other": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("want no refs, got %v", refs)
	}
}
