package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// --- pure: buildTree -----------------------------------------------------

func TestBuildTreeRecursesDirsFirst(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "SKILL.md"), "x")
	mustWrite(t, filepath.Join(dir, "README.md"), "x")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "scripts", "run.sh"), "x")

	lines, ok := buildTree(dir)
	if !ok {
		t.Fatal("buildTree should report the folder as available")
	}
	// scripts/ (dir) first, then its child indented, then files alphabetically.
	want := []struct {
		name  string
		depth int
		dir   bool
		rel   string
	}{
		{"scripts", 0, true, "scripts"},
		{"run.sh", 1, false, "scripts/run.sh"},
		{"README.md", 0, false, "README.md"},
		{"SKILL.md", 0, false, "SKILL.md"},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(lines), len(want), lines)
	}
	for i, w := range want {
		got := lines[i]
		if got.name != w.name || got.depth != w.depth || got.isDir != w.dir || got.relPath != w.rel {
			t.Errorf("line %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestBuildTreeMissingDir(t *testing.T) {
	lines, ok := buildTree(filepath.Join(t.TempDir(), "nope"))
	if ok || lines != nil {
		t.Fatalf("missing dir should be unavailable; ok=%v lines=%+v", ok, lines)
	}
}

// --- pure: classifyFile --------------------------------------------------

func TestClassifyFileText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "SKILL.md")
	mustWrite(t, p, "# Title\n")
	fc := classifyFile(p)
	if fc.kind != fileText || !fc.isMarkdown || fc.text != "# Title\n" {
		t.Fatalf("text/.md classify = %+v", fc)
	}
}

func TestClassifyFileNonMarkdownText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "run.sh")
	mustWrite(t, p, "echo hi\n")
	fc := classifyFile(p)
	if fc.kind != fileText || fc.isMarkdown {
		t.Fatalf(".sh should be text, not markdown: %+v", fc)
	}
}

func TestClassifyFileBinary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "blob.bin")
	if err := os.WriteFile(p, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	if fc := classifyFile(p); fc.kind != fileBinary {
		t.Fatalf("null bytes should classify as binary: %+v", fc)
	}
}

func TestClassifyFileTooLarge(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(p, make([]byte, maxFileSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if fc := classifyFile(p); fc.kind != fileTooLarge {
		t.Fatalf("oversized file should classify as too-large: %+v", fc)
	}
}

func TestClassifyFileError(t *testing.T) {
	if fc := classifyFile(filepath.Join(t.TempDir(), "missing")); fc.kind != fileError || fc.err == nil {
		t.Fatalf("missing file should classify as error: %+v", fc)
	}
}

// --- mode transitions ----------------------------------------------------

func TestSkillViewEnterAndBack(t *testing.T) {
	e := testEngineSkills(t, "deploy")
	m := New(e).onRefreshed(e.Refresh())

	// 'v' opens the tree for the skill under the cursor.
	m = applyKeys(m, runes("v"))
	if m.mode != modeSkillTree {
		t.Fatalf("'v' should enter modeSkillTree, got %v", m.mode)
	}
	if m.viewSkill.Name != "deploy" || !m.treeOK {
		t.Fatalf("tree should load deploy's folder: skill=%q ok=%v", m.viewSkill.Name, m.treeOK)
	}

	// esc returns to the matrix.
	esc, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if esc.(Model).mode != modeMatrix {
		t.Fatalf("esc from tree should return to matrix, got %v", esc.(Model).mode)
	}
}

func TestSkillViewOpenFileAndCascadeBack(t *testing.T) {
	e := testEngineSkills(t, "deploy")
	m := New(e).onRefreshed(e.Refresh())
	m = applyKeys(m, runes("v"))

	// The tree has a single SKILL.md; cursor is on it, enter opens it.
	enter, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = enter.(Model)
	if m.mode != modeFileView {
		t.Fatalf("enter on a file should open modeFileView, got %v", m.mode)
	}
	if m.openPath != "SKILL.md" || m.fileContent.kind != fileText {
		t.Fatalf("expected SKILL.md text open, got path=%q kind=%v", m.openPath, m.fileContent.kind)
	}

	// esc cascades: file -> tree -> matrix.
	esc1, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = esc1.(Model)
	if m.mode != modeSkillTree {
		t.Fatalf("esc from file should return to tree, got %v", m.mode)
	}
	esc2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if esc2.(Model).mode != modeMatrix {
		t.Fatalf("esc from tree should return to matrix, got %v", esc2.(Model).mode)
	}
}

func TestSkillViewEnterOnDirIsNoop(t *testing.T) {
	e := testEngineSkills(t, "deploy")
	m := New(e).onRefreshed(e.Refresh())
	// Add a subdirectory so the tree's first row is a directory.
	if err := os.MkdirAll(filepath.Join(m.skills[0].Dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	m = applyKeys(m, runes("v"))
	if line, ok := m.curTreeLine(); !ok || !line.isDir {
		t.Fatalf("expected cursor on a directory row, got %+v ok=%v", line, ok)
	}
	enter, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if enter.(Model).mode != modeSkillTree {
		t.Fatalf("enter on a directory should stay in modeSkillTree, got %v", enter.(Model).mode)
	}
}

func TestSkillViewMissingFolderShowsMetadataOnly(t *testing.T) {
	e := testEngineSkills(t, "deploy")
	m := New(e).onRefreshed(e.Refresh())
	// Point the skill at a non-existent dir to simulate an undownloaded source.
	m.skills[0].Dir = filepath.Join(t.TempDir(), "gone")
	m = applyKeys(m, runes("v"))
	if m.mode != modeSkillTree || m.treeOK {
		t.Fatalf("missing folder should open tree with treeOK=false, got mode=%v ok=%v", m.mode, m.treeOK)
	}
	if !strings.Contains(m.View(), "files unavailable") {
		t.Fatal("tree view should note files are unavailable")
	}
}

func TestSkillViewRendersWithoutPanic(t *testing.T) {
	e := testEngineSkills(t, "deploy")
	m := New(e).onRefreshed(e.Refresh())
	m.width, m.height = 80, 24
	m = applyKeys(m, runes("v"))
	if m.View() == "" {
		t.Fatal("empty tree view")
	}
	enter, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if enter.(Model).View() == "" {
		t.Fatal("empty file view")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
