package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	statusStyles = map[domain.Status]lipgloss.Style{
		domain.StatusNotInstalled:    lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		domain.StatusUpToDate:        lipgloss.NewStyle().Foreground(lipgloss.Color("78")),
		domain.StatusUpdateAvailable: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		domain.StatusConflict:        lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
	}
	statusGlyph = map[domain.Status]string{
		domain.StatusNotInstalled:    "·",
		domain.StatusUpToDate:        "=",
		domain.StatusUpdateAvailable: "↑",
		domain.StatusConflict:        "!",
	}
)

// View renders the current screen.
func (m Model) View() string {
	switch m.mode {
	case modePlan:
		return m.viewPlan()
	case modeOverwrite:
		return m.viewOverwrite()
	case modeResult:
		return m.viewResult()
	case modeConfig:
		return m.viewConfig()
	case modeForm:
		return m.viewForm()
	default:
		return m.viewMatrix()
	}
}

func (m Model) viewMatrix() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("skillmux"))
	if m.refreshing {
		b.WriteString(dimStyle.Render("  refreshing…"))
	} else if m.applying {
		b.WriteString(dimStyle.Render("  applying…"))
	}
	b.WriteString("\n\n")

	if len(m.targets) == 0 {
		b.WriteString(dimStyle.Render("No targets configured. Add some to your config.toml.\n"))
		return b.String()
	}
	if len(m.skills) == 0 {
		if m.refreshing {
			b.WriteString(dimStyle.Render("Scanning sources…\n"))
		} else {
			b.WriteString(dimStyle.Render("No skills found in the configured sources.\n"))
		}
		b.WriteString("\n" + m.footer())
		return b.String()
	}

	// A skill name offered by more than one Source is ambiguous: the user must
	// pick which Source wins per Target (selection is exclusive). Flag it.
	nameCount := map[string]int{}
	for _, s := range m.skills {
		nameCount[s.Name]++
	}
	plainLabel := func(s engine.AvailableSkill) string {
		l := s.Name + " (" + s.Source + ")"
		if nameCount[s.Name] > 1 {
			l += " ⚠"
		}
		return l
	}

	// Column widths: each target column is wide enough for its name and the
	// "[x ↑]" cell.
	const cellW = 5
	skillColW := 0
	for _, s := range m.skills {
		if w := lipgloss.Width(plainLabel(s)); w > skillColW {
			skillColW = w
		}
	}

	// Header row.
	b.WriteString(strings.Repeat(" ", skillColW+1))
	for _, t := range m.targets {
		b.WriteString(pad(t.Name, cellW+1))
	}
	b.WriteString("\n")

	// Skill rows.
	for ri, s := range m.skills {
		label := s.Name + " " + dimStyle.Render("("+s.Source+")")
		if nameCount[s.Name] > 1 {
			label += " " + statusStyles[domain.StatusConflict].Render("⚠")
		}
		b.WriteString(pad(label, skillColW+1))
		for ci, t := range m.targets {
			cell := m.renderCell(s.Name, s.Source, t.Name)
			if ri == m.row && ci == m.col {
				cell = cursorStyle.Render(cell)
			}
			b.WriteString(cell + " ")
		}
		b.WriteString("\n")
	}

	b.WriteString("\n" + m.footer())
	return b.String()
}

func (m Model) renderCell(skill, source, target string) string {
	st := m.status[statusKey{skill, source, target}]
	mark := " "
	if m.desired[reconcile.Cell{Skill: skill, Source: source, Target: target}] {
		mark = "x"
	}
	glyph := statusGlyph[st]
	if glyph == "" {
		glyph = "·"
	}
	body := fmt.Sprintf("[%s%s]", mark, statusStyles[st].Render(glyph))
	return body
}

func (m Model) footer() string {
	var b strings.Builder
	if len(m.sourceErrors) > 0 {
		names := make([]string, 0, len(m.sourceErrors))
		for n := range m.sourceErrors {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			b.WriteString(errStyle.Render(fmt.Sprintf("source %q: %v", n, m.sourceErrors[n])) + "\n")
		}
	}
	legend := dimStyle.Render("= up-to-date  ↑ update  · not-installed  ! conflict")
	keys := dimStyle.Render("↑↓←→ move · space toggle · a all · n none · r refresh · p plan · c config · q quit")
	b.WriteString(legend + "\n" + keys)
	return b.String()
}

func (m Model) viewPlan() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Plan") + "\n\n")
	if len(m.plan.Operations) == 0 {
		b.WriteString(dimStyle.Render("Nothing to do — selection already matches reality.\n"))
		b.WriteString("\n" + dimStyle.Render("press any key to go back"))
		return b.String()
	}
	for _, op := range m.plan.Operations {
		line := fmt.Sprintf("  %-9s %s", op.Kind, describeOp(op))
		switch op.Kind {
		case reconcile.Conflict:
			line = errStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + titleStyle.Render("Apply this plan? ") + dimStyle.Render("[y] yes  [n] no"))
	return b.String()
}

func describeOp(op reconcile.Operation) string {
	s := fmt.Sprintf("%s", op.SkillName)
	if op.SourceName != "" {
		s += fmt.Sprintf(" (%s)", op.SourceName)
	}
	s += " → " + op.TargetName
	if op.Reason != "" {
		s += dimStyle.Render("  ["+op.Reason+"]")
	}
	return s
}

func (m Model) viewOverwrite() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Overwrite untracked folders?") + "\n\n")
	b.WriteString(dimStyle.Render("These folders already exist but were not installed by skillmux:") + "\n\n")
	for _, c := range m.collisions {
		b.WriteString(errStyle.Render("  "+c.SkillName) +
			dimStyle.Render(" ("+c.SourceName+") → "+c.TargetName) + "\n")
		b.WriteString(dimStyle.Render("    "+c.Dir) + "\n")
	}
	b.WriteString("\n" + titleStyle.Render("Overwrite them? ") +
		dimStyle.Render("[y] yes, adopt  [n] cancel"))
	return b.String()
}

func (m Model) viewResult() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Result") + "\n\n")
	if m.applyErr != nil {
		b.WriteString(errStyle.Render("persist error: "+m.applyErr.Error()) + "\n\n")
	}
	ok, failed := 0, 0
	for _, r := range m.report.Results {
		if r.OK {
			ok++
			b.WriteString(statusStyles[domain.StatusUpToDate].Render("  ✓ ") + describeOp(r.Op) + "\n")
		} else {
			failed++
			b.WriteString(errStyle.Render("  ✗ ") + describeOp(r.Op) + errStyle.Render("  "+r.Err.Error()) + "\n")
		}
	}
	b.WriteString(fmt.Sprintf("\n%d ok, %d failed\n", ok, failed))
	b.WriteString("\n" + dimStyle.Render("press any key to continue · q to quit"))
	return b.String()
}

func (m Model) viewConfig() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Configuration") + "\n\n")

	entries := m.cfgEntries()
	if len(entries) == 0 {
		b.WriteString(dimStyle.Render("No targets or sources yet.") + "\n")
	}
	for i, e := range entries {
		kind := "target"
		if e.kind == entrySource {
			kind = "source"
		}
		line := fmt.Sprintf("%-7s %-16s %s", kind, e.name, dimStyle.Render(e.detail))
		if i == m.cfgCursor {
			line = cursorStyle.Render(fmt.Sprintf("%-7s %-16s %s", kind, e.name, e.detail))
		}
		b.WriteString("  " + line + "\n")
	}

	b.WriteString("\n" + dimStyle.Render("t add target · s add source · d delete · ↑↓ move · esc back"))
	return b.String()
}

func (m Model) viewForm() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.form.title) + "\n\n")
	for i, in := range m.form.inputs {
		marker := "  "
		if i == m.form.focus {
			marker = "> "
		}
		b.WriteString(marker + dimStyle.Render(pad(m.form.labels[i]+":", 22)) + in.View() + "\n")
	}
	if m.form.err != "" {
		b.WriteString("\n" + errStyle.Render(m.form.err) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("tab next · enter save · esc cancel"))
	return b.String()
}

// pad right-pads s to width w (ignoring ANSI styling width discrepancies for
// simple labels).
func pad(s string, w int) string {
	gap := w - lipgloss.Width(s)
	if gap < 0 {
		gap = 0
	}
	return s + strings.Repeat(" ", gap)
}
