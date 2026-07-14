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
// files are tiny in practice; this bounds the off-loop read+render (see
// renderFileCmd) so a pathologically large text file can't exhaust memory (a
// binary of any size is caught earlier by the binary check).
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
	m.viewEdges = m.skillEdges(sk)
	m.viewTree, m.treeOK = buildTree(sk.Dir)
	m.treeCursor, m.treeScroll = 0, 0
	m.viewMsg = ""
	m.mode = modeSkillTree
	// Tell the engine this Source's files are being read, so a concurrent
	// Refresh defers rewriting its working tree until the view closes.
	m.eng.BeginView(sk.Source)
	return m
}

// leaveSkillView returns to the matrix and, if a Refresh deferred this Source's
// checkout while the view was open, kicks a catch-up Refresh to apply it now
// that reads have stopped — serialised behind any Refresh still in flight.
func (m Model) leaveSkillView() (tea.Model, tea.Cmd) {
	m.mode = modeMatrix
	if !m.eng.EndView() {
		return m, nil
	}
	if m.refreshing {
		m.pendingRefresh = true
		return m, nil
	}
	m.refreshing = true
	return m, refreshCmd(m.eng)
}

// navLen is the length of the skill view's navigable list: its outgoing edges
// followed by its file-tree rows. treeCursor indexes this combined list.
func (m Model) navLen() int { return len(m.viewEdges) + len(m.viewTree) }

// curEdge returns the edge under the cursor, if the cursor is in the edges
// section (the first len(viewEdges) rows).
func (m Model) curEdge() (skillEdge, bool) {
	if m.treeCursor >= 0 && m.treeCursor < len(m.viewEdges) {
		return m.viewEdges[m.treeCursor], true
	}
	return skillEdge{}, false
}

func (m Model) onSkillTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		return m.leaveSkillView()
	case "up", "k":
		m.treeCursor--
	case "down", "j":
		m.treeCursor++
	case "home", "g":
		m.treeCursor = 0
	case "end", "G":
		m.treeCursor = m.navLen() - 1
	case "t":
		return m.toggleEdge(), nil
	case "enter":
		if line, ok := m.curTreeLine(); ok && !line.isDir {
			return m.openFile(line.relPath)
		}
	}
	m.clampTreeCursor()
	return m, nil
}

// toggleEdge flips the focused edge between Dependency and Suggestion and
// persists it to Config. A router-wide bulk Suggestion cannot be lifted per
// edge, so it steers the user to the hand-editable TOML instead. A no-op when
// the cursor is not on an edge.
func (m Model) toggleEdge() Model {
	e, ok := m.curEdge()
	if !ok {
		return m
	}
	if e.bulk {
		m.viewMsg = "router-wide suggestion — edit the [[suggestion]] entry in config.toml to change"
		return m
	}
	now, err := m.eng.ToggleSuggestion(m.viewSkill.Name, e.to)
	if err != nil {
		m.viewMsg = "could not save: " + err.Error()
		return m
	}
	// The Config changed, so the graph's edge classification is stale — rebuild
	// it before re-rendering the edges.
	m.graph = m.eng.SkillGraph(m.cat)
	m.viewEdges = m.skillEdges(m.viewSkill) // reflect the new classification
	kind := "Dependency"
	if now {
		kind = "Suggestion"
	}
	m.viewMsg = fmt.Sprintf("%s → %s is now a %s", m.viewSkill.Name, e.to, kind)
	return m
}

// fileRenderedMsg carries the result of an off-loop file read+render back to the
// Update loop. path is the relative path the render was for, so a result for a
// file the user has already navigated away from can be discarded.
type fileRenderedMsg struct {
	path    string
	content fileContent
	body    string
}

// openFile switches to the file viewer and kicks off an off-loop read+render of
// the file at relPath. The view shows a loading note until the fileRenderedMsg
// lands, so a slow glamour render never freezes the matrix.
func (m Model) openFile(relPath string) (tea.Model, tea.Cmd) {
	path := filepath.Join(m.viewSkill.Dir, filepath.FromSlash(relPath))
	w, _ := m.fileViewerSize()
	m.openPath = relPath
	m.fileLoading = true
	m.fileContent = fileContent{}
	m.fileVP = viewport.Model{}
	m.mode = modeFileView
	return m, renderFileCmd(path, relPath, w)
}

// renderFileCmd reads and renders a file in a goroutine, posting the result as a
// fileRenderedMsg. classifyFile and the glamour markdown render (the expensive
// step) run here, off the UI loop — this is what keeps the file view responsive.
func renderFileCmd(path, relPath string, width int) tea.Cmd {
	return func() tea.Msg {
		fc := classifyFile(path)
		return fileRenderedMsg{path: relPath, content: fc, body: renderBody(fc, width)}
	}
}

// onFileRendered installs a completed off-loop render, discarding a stale result
// for a file the user has already left (esc'd back, or opened another file).
func (m Model) onFileRendered(msg fileRenderedMsg) Model {
	if m.mode != modeFileView || msg.path != m.openPath {
		return m
	}
	m.fileLoading = false
	m.fileContent = msg.content
	w, h := m.fileViewerSize()
	if h <= 0 {
		h = 1 // viewport needs a positive height even in unbounded mode
	}
	vp := viewport.New(w, h)
	vp.SetContent(msg.body)
	m.fileVP = vp
	return m
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

// curTreeLine returns the file-tree row under the cursor, if the cursor is in
// the files section (the rows after the edges).
func (m Model) curTreeLine() (treeLine, bool) {
	fi := m.treeCursor - len(m.viewEdges)
	if fi < 0 || fi >= len(m.viewTree) {
		return treeLine{}, false
	}
	return m.viewTree[fi], true
}

// clampTreeCursor keeps the cursor in range over the combined edges-then-files
// list. The edges block is small and always rendered in full; only the file
// tree scrolls, so treeScroll is a window over the file rows alone. When the
// cursor sits in the edges block the file tree rests at its top.
func (m *Model) clampTreeCursor() {
	n := m.navLen()
	if m.treeCursor < 0 {
		m.treeCursor = 0
	}
	if m.treeCursor >= n {
		m.treeCursor = max(0, n-1)
	}

	nEdges := len(m.viewEdges)
	if m.treeCursor < nEdges {
		m.treeScroll = 0
		return
	}
	fc := m.treeCursor - nEdges // cursor's index within the file tree
	vis := m.treeVisibleRows()
	if fc < m.treeScroll {
		m.treeScroll = fc
	}
	if fc >= m.treeScroll+vis {
		m.treeScroll = fc - vis + 1
	}
	if hi := len(m.viewTree) - vis; m.treeScroll > hi {
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
		lines = append(lines, dimStyle.Render(sanitize(m.viewSkill.Description)))
	}
	for _, t := range m.targets {
		st := m.status[statusKey{m.viewSkill.Name, m.viewSkill.Source, t.Name}]
		glyph := statusGlyph[st]
		if glyph == "" {
			glyph = "·"
		}
		lines = append(lines, "  "+statusStyles[st].Render(glyph+" "+statusText(st))+"  "+dimStyle.Render(t.Name))
	}
	lines = append(lines, dimStyle.Render(sanitize(m.viewSkill.Dir)))
	if rev, ok := m.cat.Revisions[m.viewSkill.Source]; ok {
		lines = append(lines, dimStyle.Render(sanitize(rev.Label())))
	}
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
	// header(1) + blank(1) + metadata + rule(1) + Dependencies block + footer.
	avail := h - 2 - len(m.treeMetaLines()) - 1 - lipgloss.Height(m.skillTreeFooter())
	if n := len(m.viewEdges); n > 0 {
		avail -= 1 + n + 1 + 1 // "Dependencies" header + edges + spacer + "Files" header
	}
	if m.viewMsg != "" {
		avail-- // transient note line below the tree
	}
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

// renderBody is the text the file viewport scrolls: rendered markdown for a .md
// file, raw text for any other text file, or a descriptive note otherwise. The
// glamour render is the expensive step, so renderBody is called off the UI loop
// (see renderFileCmd), never from the synchronous Update path.
func renderBody(fc fileContent, width int) string {
	switch fc.kind {
	case fileBinary:
		return dimStyle.Render(fmt.Sprintf("(binary, %d bytes)", fc.size))
	case fileTooLarge:
		return dimStyle.Render(fmt.Sprintf("(file too large, %d bytes)", fc.size))
	case fileError:
		return errStyle.Render("(could not read: " + sanitize(fc.err.Error()) + ")")
	default:
		// fc.text is Source-controlled; make it inert before it reaches Glamour
		// or the raw viewport so no escape sequence in the file can act.
		text := sanitizeMultiline(fc.text)
		if fc.isMarkdown {
			if out, err := renderMarkdown(text, width); err == nil {
				return out
			}
			// Fall back to raw text if glamour fails for any reason.
		}
		return text
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
