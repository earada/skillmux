package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// change looks up a path in a Summary, failing the test when it is absent.
func change(t *testing.T, s Summary, path string) FileChange {
	t.Helper()
	for _, c := range s.Changes {
		if c.Path == path {
			return c
		}
	}
	t.Fatalf("no change reported for %q; got %+v", path, s.Changes)
	return FileChange{}
}

func TestCompareIdenticalFoldersAreEmpty(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	for _, d := range []string{oldDir, newDir} {
		write(t, d, "SKILL.md", "# deploy\n")
		write(t, d, "scripts/run.sh", "echo hi\n")
	}
	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Empty() {
		t.Fatalf("identical folders should compare equal, got %+v", s.Changes)
	}
}

func TestCompareClassifiesAddedRemovedModified(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write(t, oldDir, "SKILL.md", "one\ntwo\n")
	write(t, newDir, "SKILL.md", "one\nTWO\n")
	write(t, oldDir, "gone.md", "bye\n")
	write(t, newDir, "scripts/new.sh", "echo new\n")

	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	added, removed, modified := s.Counts()
	if added != 1 || removed != 1 || modified != 1 {
		t.Fatalf("counts = %d added / %d removed / %d modified: %+v", added, removed, modified, s.Changes)
	}
	// Sorted by path: gone.md, scripts/new.sh, SKILL.md (uppercase sorts first).
	if s.Changes[0].Path != "SKILL.md" {
		t.Fatalf("changes should be sorted by path, got %+v", s.Changes)
	}

	if c := change(t, s, "SKILL.md"); c.Kind != Modified || c.Adds != 1 || c.Dels != 1 {
		t.Fatalf("SKILL.md = %+v", c)
	}
	rm := change(t, s, "gone.md")
	if rm.Kind != Removed || rm.Dels != 1 || len(rm.Hunks) != 1 {
		t.Fatalf("gone.md = %+v", rm)
	}
	if rm.Hunks[0].Lines[0].Kind != Del || rm.Hunks[0].Lines[0].Text != "bye" {
		t.Fatalf("a removed file's content should read as deletions: %+v", rm.Hunks[0])
	}
	add := change(t, s, "scripts/new.sh")
	if add.Kind != Added || add.Adds != 1 || add.Hunks[0].Lines[0].Kind != Add {
		t.Fatalf("scripts/new.sh = %+v", add)
	}
}

// Same size, different bytes: the fast size check must not be mistaken for a
// content check.
func TestCompareDetectsSameSizeEdit(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write(t, oldDir, "SKILL.md", "abc\n")
	write(t, newDir, "SKILL.md", "abd\n")

	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Changes) != 1 || s.Changes[0].Kind != Modified {
		t.Fatalf("same-size edit should be reported as modified, got %+v", s.Changes)
	}
}

func TestCompareBinaryFileIsNotedNotDumped(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "logo.png"), []byte{0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "logo.png"), []byte{0x00, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	c := change(t, s, "logo.png")
	if len(c.Hunks) != 0 || !strings.Contains(c.Note, "binary") {
		t.Fatalf("binary file should be noted, not diffed: %+v", c)
	}
}

func TestCompareOversizedFileIsNoted(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	big := strings.Repeat("a", maxFileSize+1)
	write(t, oldDir, "big.txt", big)
	write(t, newDir, "big.txt", big+"b")

	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	c := change(t, s, "big.txt")
	if len(c.Hunks) != 0 || !strings.Contains(c.Note, "too large") {
		t.Fatalf("oversized file should be noted, not diffed: %+v", c)
	}
}

// A trailing-newline-only difference is real (the bytes differ) but has no line
// diff to show, so it must be reported with a note rather than an empty change.
func TestCompareTrailingNewlineOnlyIsNoted(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write(t, oldDir, "SKILL.md", "one\n")
	write(t, newDir, "SKILL.md", "one")

	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	c := change(t, s, "SKILL.md")
	if len(c.Hunks) != 0 || !strings.Contains(c.Note, "trailing newline") {
		t.Fatalf("expected a trailing-newline note, got %+v", c)
	}
}

func TestCompareIgnoresSymlinks(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write(t, oldDir, "SKILL.md", "x\n")
	write(t, newDir, "SKILL.md", "x\n")
	if err := os.Symlink(filepath.Join(newDir, "SKILL.md"), filepath.Join(newDir, "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	// Symlinks are outside the fingerprint's notion of content, so adding one is
	// not a change — otherwise the diff would disagree with the drift detection.
	if !s.Empty() {
		t.Fatalf("a new symlink should not register as a change, got %+v", s.Changes)
	}
}

func TestCompareMissingSideErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Compare(filepath.Join(dir, "nope"), dir); err == nil {
		t.Fatal("a missing old folder should error")
	}
	if _, err := Compare(dir, filepath.Join(dir, "nope")); err == nil {
		t.Fatal("a missing new folder should error")
	}
	file := filepath.Join(dir, "SKILL.md")
	write(t, dir, "SKILL.md", "x")
	if _, err := Compare(file, dir); err == nil {
		t.Fatal("a file where a folder is expected should error")
	}
}

func TestCompareEmptyAddedFileIsNoted(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write(t, oldDir, "SKILL.md", "x\n")
	write(t, newDir, "SKILL.md", "x\n")
	write(t, newDir, "empty.txt", "")

	s, err := Compare(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	c := change(t, s, "empty.txt")
	if c.Kind != Added || len(c.Hunks) != 0 || c.Note != "empty file" {
		t.Fatalf("empty added file = %+v", c)
	}
}
