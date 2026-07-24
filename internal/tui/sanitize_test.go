package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/engine"
)

// --- boundary: renderBody makes file contents and read errors inert -------

func TestRenderBodyRawTextIsInert(t *testing.T) {
	fc := fileContent{kind: fileText, text: "top\n\x1b]52;c;x\x07\rbottom"}
	got := renderBody(fc, 80, "dark")
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) || strings.ContainsRune(got, '\r') {
		t.Fatalf("renderBody leaked a control byte: %q", got)
	}
}

func TestRenderBodyMarkdownIsInert(t *testing.T) {
	fc := fileContent{kind: fileText, isMarkdown: true, text: "# Title\n\n\x1b]0;pwned\x07body"}
	got := renderBody(fc, 80, "dark")
	if strings.Contains(got, "\x1b]0;pwned") || strings.ContainsRune(got, 0x07) {
		t.Fatalf("markdown render leaked the OSC sequence: %q", got)
	}
}

func TestRenderBodyReadErrorIsInert(t *testing.T) {
	fc := fileContent{kind: fileError, err: errors.New("open \x1b]0;evil\x07foo: denied")}
	got := renderBody(fc, 80, "dark")
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) {
		t.Fatalf("file error leaked an escape byte: %q", got)
	}
}

// --- boundary: skill label / filename / description -----------------------

func TestSkillLabelSanitizesNameGroupAndSource(t *testing.T) {
	s := engine.AvailableSkill{
		Name:   "cool\x1b]0;pwn\x07skill",
		Group:  "grp\x1b[31m",
		Source: "src\rspoof",
	}
	got := skillLabel(s, false)
	for _, bad := range []rune{0x1b, 0x07, '\r'} {
		if strings.ContainsRune(got, bad) {
			t.Fatalf("skillLabel leaked %#x: %q", bad, got)
		}
	}
}

func TestTreeRowSanitizesFilename(t *testing.T) {
	m := Model{}
	got := m.treeRow(treeLine{name: "readme\x1b]52;c;x\x07.md"}, false)
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) {
		t.Fatalf("treeRow leaked an escape byte: %q", got)
	}
}
