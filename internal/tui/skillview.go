package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/earada/skillmux/internal/domain"
)

// maxFileSize caps how large a file modeFileView will read into memory. Skill
// files are tiny in practice; this guards the synchronous read+render path from
// freezing the UI on a pathologically large text file (a binary of any size is
// caught earlier by the binary check). See ADR-less design note in CONTEXT.
const maxFileSize = 1 << 20 // 1 MiB

// treeLine is one row of a Skill's recursive file tree: its display name, how
// deep it sits (for indentation), whether it is a directory, and its path
// relative to the Skill folder (used to open a file).
type treeLine struct {
	depth   int
	name    string
	isDir   bool
	relPath string
}

// buildTree walks a Skill's folder depth-first and returns its contents as a
// flat, indent-aware list (directories first then files, alphabetical within
// each level, always expanded). The bool is false when the folder is missing —
// the common best-effort case where a remote Source hasn't been downloaded yet
// (or its cache was cleared); the caller shows metadata only.
func buildTree(dir string) ([]treeLine, bool) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, false
	}
	var lines []treeLine
	var walk func(path, rel string, depth int)
	walk = func(path, rel string, depth int) {
		ents, err := os.ReadDir(path)
		if err != nil {
			return
		}
		sort.Slice(ents, func(i, j int) bool {
			if di, dj := ents[i].IsDir(), ents[j].IsDir(); di != dj {
				return di // directories before files
			}
			return ents[i].Name() < ents[j].Name()
		})
		for _, e := range ents {
			childRel := e.Name()
			if rel != "" {
				childRel = rel + "/" + e.Name()
			}
			lines = append(lines, treeLine{depth: depth, name: e.Name(), isDir: e.IsDir(), relPath: childRel})
			if e.IsDir() {
				walk(filepath.Join(path, e.Name()), childRel, depth+1)
			}
		}
	}
	walk(dir, "", 0)
	return lines, true
}

// fileKind classifies a file the moment modeFileView opens it.
type fileKind int

const (
	fileText     fileKind = iota // UTF-8 text, small enough to show
	fileBinary                   // null bytes or invalid UTF-8
	fileTooLarge                 // exceeds maxFileSize
	fileError                    // stat/read failed
)

// fileContent is the result of classifying and (when text) reading a file.
type fileContent struct {
	kind       fileKind
	text       string // populated only for fileText
	size       int64
	err        error // populated only for fileError
	isMarkdown bool  // a .md file, so modeFileView renders it with glamour
}

// classifyFile stats and (when appropriate) reads a file, deciding how
// modeFileView should present it. Binaries and oversized files are described
// rather than dumped; a read error is reported, never fatal.
func classifyFile(path string) fileContent {
	info, err := os.Stat(path)
	if err != nil {
		return fileContent{kind: fileError, err: err}
	}
	if info.Size() > maxFileSize {
		return fileContent{kind: fileTooLarge, size: info.Size()}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileContent{kind: fileError, err: err}
	}
	if isBinary(data) {
		return fileContent{kind: fileBinary, size: info.Size()}
	}
	return fileContent{
		kind:       fileText,
		text:       string(data),
		size:       info.Size(),
		isMarkdown: strings.EqualFold(filepath.Ext(path), ".md"),
	}
}

// isBinary reports whether data looks non-textual: a null byte or invalid UTF-8
// is enough to treat it as binary and avoid dumping garbage into the viewport.
func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
}

// --- skill-view navigation ----------------------------------------------

// enterSkillView opens the read-only explorer for the Skill under the cursor:
// it walks the folder (best-effort) and switches to the tree screen.
func (m Model) enterSkillView() Model {
	sk, ok := m.curSkill()
	if !ok {
		return m
	}
	m.viewSkill = sk
	m.viewTree, m.treeOK = buildTree(sk.Dir)
	m.treeCursor, m.treeScroll = 0, 0
	m.mode = modeSkillTree
	return m
}

func (m Model) onSkillTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.mode = modeMatrix
		return m, nil
	case "up", "k":
		m.treeCursor--
	case "down", "j":
		m.treeCursor++
	case "home", "g":
		m.treeCursor = 0
	case "end", "G":
		m.treeCursor = len(m.viewTree) - 1
	case "enter":
		if line, ok := m.curTreeLine(); ok && !line.isDir {
			return m.openFile(line.relPath)
		}
	}
	m.clampTreeCursor()
	return m, nil
}

// openFile classifies the file at relPath within the current Skill and switches
// to the scrollable file viewer.
func (m Model) openFile(relPath string) (tea.Model, tea.Cmd) {
	path := filepath.Join(m.viewSkill.Dir, filepath.FromSlash(relPath))
	m.openPath = relPath
	m.fileContent = classifyFile(path)
	m.fileVP = m.newFileViewport()
	m.mode = modeFileView
	return m, nil
}

func (m Model) onFileViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.mode = modeSkillTree
		return m, nil
	}
	// Everything else (↑/↓, pgup/pgdown, etc.) drives the viewport's scroll.
	var cmd tea.Cmd
	m.fileVP, cmd = m.fileVP.Update(msg)
	return m, cmd
}

// curTreeLine returns the tree row under the cursor, if any.
func (m Model) curTreeLine() (treeLine, bool) {
	if m.treeCursor < 0 || m.treeCursor >= len(m.viewTree) {
		return treeLine{}, false
	}
	return m.viewTree[m.treeCursor], true
}

// clampTreeCursor keeps the tree cursor in range and inside the scroll window,
// mirroring clampCursor for the matrix.
func (m *Model) clampTreeCursor() {
	n := len(m.viewTree)
	if m.treeCursor < 0 {
		m.treeCursor = 0
	}
	if m.treeCursor >= n {
		m.treeCursor = max(0, n-1)
	}
	vis := m.treeVisibleRows()
	if m.treeCursor < m.treeScroll {
		m.treeScroll = m.treeCursor
	}
	if m.treeCursor >= m.treeScroll+vis {
		m.treeScroll = m.treeCursor - vis + 1
	}
	if hi := n - vis; m.treeScroll > hi {
		m.treeScroll = hi
	}
	if m.treeScroll < 0 {
		m.treeScroll = 0
	}
}

// treeMetaLines is the metadata block shown above the file tree: the Skill
// label, its description, its per-Target status, and the folder it lives in.
// Rendering and treeVisibleRows both call this so the scroll arithmetic agrees.
func (m Model) treeMetaLines() []string {
	lines := []string{skillLabel(m.viewSkill, false)}
	if m.viewSkill.Description != "" {
		lines = append(lines, dimStyle.Render(m.viewSkill.Description))
	}
	for _, t := range m.targets {
		st := m.status[statusKey{m.viewSkill.Name, m.viewSkill.Source, t.Name}]
		glyph := statusGlyph[st]
		if glyph == "" {
			glyph = "·"
		}
		lines = append(lines, "  "+statusStyles[st].Render(glyph+" "+statusText(st))+"  "+dimStyle.Render(t.Name))
	}
	lines = append(lines, dimStyle.Render(m.viewSkill.Dir))
	return lines
}

// statusText is the human label for a cell Status, shown in the skill view's
// metadata block (the matrix uses glyphs alone).
func statusText(s domain.Status) string {
	switch s {
	case domain.StatusUpToDate:
		return "up-to-date"
	case domain.StatusUpdateAvailable:
		return "update available"
	case domain.StatusConflict:
		return "conflict"
	default:
		return "not installed"
	}
}

// treeVisibleRows is how many tree rows fit below the metadata block, given the
// terminal height; it keeps clampTreeCursor's scroll window in sync with the
// renderer. Unbounded height (tests / first frame) shows the whole tree.
func (m Model) treeVisibleRows() int {
	_, h := m.dims()
	if h <= 0 {
		return max(1, len(m.viewTree))
	}
	// header(1) + blank(1) + metadata + rule(1) + footer.
	avail := h - 2 - len(m.treeMetaLines()) - 1 - lipgloss.Height(m.skillTreeFooter())
	if avail < 1 {
		avail = 1
	}
	return avail
}

// fileViewerSize is the inner width/height available to the file viewport,
// given the terminal size and the breadcrumb header + footer chrome.
func (m Model) fileViewerSize() (w, h int) {
	w, termH := m.dims()
	w -= 4 // panel border (2) + horizontal padding (2)
	if w < 1 {
		w = 1
	}
	if termH <= 0 {
		return w, 0 // unbounded (tests / first frame)
	}
	footerH := lipgloss.Height(m.fileFooter())
	// header(1) + blank(1) + breadcrumb(1) + panel border top/bottom(2) + footer.
	h = termH - 2 - 1 - 2 - footerH
	if h < 1 {
		h = 1
	}
	return w, h
}

// newFileViewport builds the viewport for the open file, rendering markdown
// through glamour and showing a one-line note for binary / oversized / errored
// files instead of their bytes.
func (m Model) newFileViewport() viewport.Model {
	w, h := m.fileViewerSize()
	if h <= 0 {
		h = 1 // viewport needs a positive height even in unbounded mode
	}
	vp := viewport.New(w, h)
	vp.SetContent(m.fileBody(w))
	return vp
}

// fileBody is the text the file viewport scrolls: rendered markdown for a .md
// file, raw text for any other text file, or a descriptive note otherwise.
func (m Model) fileBody(width int) string {
	switch m.fileContent.kind {
	case fileBinary:
		return dimStyle.Render(fmt.Sprintf("(binary, %d bytes)", m.fileContent.size))
	case fileTooLarge:
		return dimStyle.Render(fmt.Sprintf("(file too large, %d bytes)", m.fileContent.size))
	case fileError:
		return errStyle.Render("(could not read: " + m.fileContent.err.Error() + ")")
	default:
		if m.fileContent.isMarkdown {
			if out, err := renderMarkdown(m.fileContent.text, width); err == nil {
				return out
			}
			// Fall back to raw text if glamour fails for any reason.
		}
		return m.fileContent.text
	}
}

// renderMarkdown styles markdown for the terminal with glamour, wrapping to the
// viewport width so lines don't overrun the panel.
func renderMarkdown(md string, width int) (string, error) {
	if width < 1 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))
	if err != nil {
		return "", err
	}
	out, err := r.Render(md)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}
