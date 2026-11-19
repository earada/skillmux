package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildTreeHidesVCSMetadata covers skillmux-ia0: the skill explorer must
// not list a `.git` directory or gitfile, matching what is installed.
func TestBuildTreeHidesVCSMetadata(t *testing.T) {
	dir := t.TempDir()
	for rel, content := range map[string]string{
		"SKILL.md":  "body",
		".git/HEAD": "ref\n",
		"sub/.git":  "gitdir: x\n",
		"sub/a.txt": "a",
	} {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lines, ok := buildTree(dir)
	if !ok {
		t.Fatal("buildTree reported missing dir")
	}
	for _, l := range lines {
		if l.name == ".git" {
			t.Errorf("tree lists VCS metadata %q", l.relPath)
		}
	}
	if len(lines) != 3 { // sub/, sub/a.txt, SKILL.md
		t.Errorf("want 3 lines, got %+v", lines)
	}
}
