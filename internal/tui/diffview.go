package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/earada/skillmux/internal/diff"
	"github.com/earada/skillmux/internal/engine"
)

// The diff screen (modeDiff) answers "what would a reinstall change?" before the
// user commits to one: the installed copy at a Target on the old side, the
// Skill's current Source folder on the new side (see ADR 0007). It is reachable
// from the skill view ('d', against the Target column the matrix cursor sits on)
// and from the Plan ('d', against the operation under the cursor).

// diffKey identifies what a diff computation is for, so a result that lands after
// the user has navigated elsewhere can be discarded.
type diffKey struct{ skill, source, target string }

// diffComputedMsg carries the result of an off-loop comparison back to the
// Update loop, already rendered (the render walks every hunk, so it does not
// belong on the UI loop either).
type diffComputedMsg struct {
	key  diffKey
	comp engine.Comparison
	err  error
	body string
}

// enterDiff opens the diff screen for sk against target and kicks off the
// comparison off-loop. back is the screen esc returns to.
func (m Model) enterDiff(sk engine.AvailableSkill, target string, back viewMode) (Model, tea.Cmd) {
	key := diffKey{sk.Name, sk.Source, target}
	m.diffBack = back
	m.diffKey = key
	m.diffTitle = sk.Name + " (" + sk.Source + ") → " + target
	m.diffLoading = true
	m.diffComp = engine.Comparison{}
	m.diffErr = nil
	m.diffVP = viewport.Model{}
	m.mode = modeDiff
	w, _ := m.diffViewerSize()
	return m, computeDiffCmd(m.eng, sk, target, key, w)
}

// computeDiffCmd compares the two folders and renders the result in a goroutine,
// posting a diffComputedMsg. Both the file walk and the hunk rendering happen
// here, off the UI loop, so a large Skill never freezes the screen.
func computeDiffCmd(e *engine.Engine, sk engine.AvailableSkill, target string, key diffKey, width int) tea.Cmd {
	return func() tea.Msg {
		comp, err := e.Compare(sk, target)
		msg := diffComputedMsg{key: key, comp: comp, err: err}
		if err == nil {
			msg.body = renderDiff(comp, width)
		}
		return msg
	}
}

// onDiffComputed installs a completed comparison, discarding a stale result for a
// diff the user has already left (esc'd back, or opened another one).
func (m Model) onDiffComputed(msg diffComputedMsg) Model {
	if m.mode != modeDiff || msg.key != m.diffKey {
		return m
	}
	m.diffLoading = false
	m.diffComp = msg.comp
	m.diffErr = msg.err
	body := msg.body
	if msg.err != nil {
		body = errStyle.Render("could not compare: " + sanitize(msg.err.Error()))
	}
	w, h := m.diffViewerSize()
	if h <= 0 {
		h = 1 // a viewport needs a positive height even in unbounded mode
	}
	vp := viewport.New(w, h)
	vp.SetContent(body)
	m.diffVP = vp
	return m
}

func (m Model) onDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.mode = m.diffBack
		return m, nil
	}
	// Everything else (↑/↓, pgup/pgdown, …) drives the viewport's scroll.
	var cmd tea.Cmd
	m.diffVP, cmd = m.diffVP.Update(msg)
	return m, cmd
}

// diffViewerSize is the inner width/height available to the diff viewport, given
// the terminal size and the breadcrumb header + footer chrome. It mirrors
// fileViewerSize; the two screens share their layout.
func (m Model) diffViewerSize() (w, h int) {
	return m.fileViewerSize()
}

// enterDiffFromSkillView opens the diff for the Skill being explored against the
// Target the matrix cursor sits on. A Target with no copy of the Skill has
// nothing to compare, which is reported inline rather than by opening an empty
// screen.
func (m Model) enterDiffFromSkillView() (tea.Model, tea.Cmd) {
	if m.col < 0 || m.col >= len(m.targets) {
		m.viewMsg = "no target to compare against"
		return m, nil
	}
	target := m.targets[m.col].Name
	if _, ok := m.eng.InstalledCopy(target, m.viewSkill.Name); !ok {
		m.viewMsg = fmt.Sprintf("%s is not installed in %s — nothing to compare", m.viewSkill.Name, target)
		return m, nil
	}
	if m.viewSkill.Dir == "" {
		m.viewMsg = fmt.Sprintf("%s is not available from %s — nothing to compare against", m.viewSkill.Name, m.viewSkill.Source)
		return m, nil
	}
	return m.enterDiff(m.viewSkill, target, modeSkillTree)
}

// enterDiffFromPlan opens the diff for the operation under the Plan's cursor, so
// a reinstall (or an overwrite of an untracked folder) can be inspected before
// applying. Anything with no old side to compare is reported inline.
func (m Model) enterDiffFromPlan() (tea.Model, tea.Cmd) {
	op, ok := m.curPlanOp()
	if !ok {
		return m, nil
	}
	if _, ok := m.eng.InstalledCopy(op.TargetName, op.SkillName); !ok {
		m.planMsg = fmt.Sprintf("%s has no copy in %s yet — nothing to compare", op.SkillName, op.TargetName)
		return m, nil
	}
	sk, ok := m.catalogSkill(op.SkillName, op.SourceName)
	if !ok {
		m.planMsg = fmt.Sprintf("%s is no longer offered by %s — nothing to compare against", op.SkillName, op.SourceName)
		return m, nil
	}
	return m.enterDiff(sk, op.TargetName, modePlan)
}

// catalogSkill finds an available Skill by its (name, source) identity.
func (m Model) catalogSkill(name, source string) (engine.AvailableSkill, bool) {
	for _, sk := range m.cat.Skills {
		if sk.Name == name && sk.Source == source {
			return sk, true
		}
	}
	return engine.AvailableSkill{}, false
}

// --- rendering -----------------------------------------------------------

// renderDiff is the scrollable body of the diff screen: how much the old side can
// be trusted, a per-file summary, then the unified hunks. Every interpolated
// string — paths, file names, file contents — is Source- or Target-controlled, so
// it is sanitized and clipped to the width before it reaches the terminal.
func renderDiff(c engine.Comparison, width int) string {
	clip := lipgloss.NewStyle().MaxWidth(max(1, width))
	var b strings.Builder

	switch {
	case !c.Tracked:
		b.WriteString(clip.Render(errStyle.Render("⚠ untracked — skillmux did not install this copy; applying would overwrite it")) + "\n")
	case !c.Pristine:
		b.WriteString(clip.Render(errStyle.Render("≠ modified locally — this copy has hand-made edits, so the changes below mix local and upstream")) + "\n")
	default:
		b.WriteString(clip.Render(dimStyle.Render("what a reinstall would change — installed copy vs upstream")) + "\n")
	}
	b.WriteString(clip.Render(diffDelStyle.Render("--- "+sanitize(c.InstalledDir))) + "\n")
	b.WriteString(clip.Render(diffAddStyle.Render("+++ "+sanitize(c.SourceDir))) + "\n\n")

	if c.Summary.Empty() {
		b.WriteString(dimStyle.Render("no differences — the installed copy already matches upstream"))
		return b.String()
	}

	added, removed, modified := c.Summary.Counts()
	b.WriteString(clip.Render(headingStyle.Render(fmt.Sprintf("%d file(s) changed", len(c.Summary.Changes)))+
		dimStyle.Render(fmt.Sprintf("  %d added · %d removed · %d modified", added, removed, modified))) + "\n")
	for _, ch := range c.Summary.Changes {
		b.WriteString(clip.Render(changeSummaryRow(ch)) + "\n")
	}

	for _, ch := range c.Summary.Changes {
		b.WriteString("\n" + clip.Render(changeHeader(ch)) + "\n")
		if ch.Note != "" {
			b.WriteString(clip.Render(dimStyle.Render("  ("+sanitize(ch.Note)+")")) + "\n")
			continue
		}
		for _, h := range ch.Hunks {
			b.WriteString(clip.Render(diffHunkStyle.Render(h.Header())) + "\n")
			for _, l := range h.Lines {
				b.WriteString(clip.Render(diffLine(l)) + "\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// changeSummaryRow is one line of the file list above the hunks: the change glyph,
// the path, and the ± line counts.
func changeSummaryRow(ch diff.FileChange) string {
	row := changeGlyphStyle(ch.Kind).Render(ch.Kind.Glyph()+" ") + sanitize(ch.Path)
	switch {
	case ch.Note != "":
		row += dimStyle.Render("  " + sanitize(ch.Note))
	case ch.Adds > 0 || ch.Dels > 0:
		row += "  " + diffAddStyle.Render(fmt.Sprintf("+%d", ch.Adds)) +
			" " + diffDelStyle.Render(fmt.Sprintf("-%d", ch.Dels))
	}
	return "  " + row
}

// changeHeader titles a file's hunks in the per-file section below the summary.
func changeHeader(ch diff.FileChange) string {
	return changeGlyphStyle(ch.Kind).Render(ch.Kind.Glyph()+" "+sanitize(ch.Path)) +
		dimStyle.Render("  "+string(ch.Kind))
}

func changeGlyphStyle(k diff.Kind) lipgloss.Style {
	switch k {
	case diff.Added:
		return diffAddStyle
	case diff.Removed:
		return diffDelStyle
	default:
		return diffHunkStyle
	}
}

// diffLine renders one content line: '+' green, '-' red, context dimmed. The text
// is file content from a Source, so it is sanitized before styling.
func diffLine(l diff.Line) string {
	body := l.Kind.Prefix() + sanitize(l.Text)
	switch l.Kind {
	case diff.Add:
		return diffAddStyle.Render(body)
	case diff.Del:
		return diffDelStyle.Render(body)
	default:
		return dimStyle.Render(body)
	}
}
