package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/reconcile"
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

// --- layout primitives ---------------------------------------------------

// dims returns the usable terminal size, falling back to a sane width when no
// WindowSizeMsg has arrived yet (e.g. in tests). A height of 0 means "unbounded":
// render everything without scrolling or bottom-padding.
func (m Model) dims() (w, h int) {
	w, h = m.width, m.height
	if w <= 0 {
		w = 80
	}
	return w, h
}

// headerBar is the brand pill on the left with an optional status on the right.
func (m Model) headerBar(status string) string {
	w, _ := m.dims()
	left := titleStyle.Render("skillmux")
	right := ""
	if status != "" {
		right = dimStyle.Render(status)
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// frame stacks header, body and footer, pushing the footer to the bottom of the
// screen when the height is known.
func (m Model) frame(header, body, footer string) string {
	w, h := m.dims()
	// Truncate every line to the terminal width so a long footer/header can't
	// wrap and throw off the height arithmetic (and the scroll window).
	clip := lipgloss.NewStyle().MaxWidth(w)
	header, footer = clip.Render(header), clip.Render(footer)
	out := header + "\n\n" + body
	if footer == "" {
		return out
	}
	if h > 0 {
		gap := h - lipgloss.Height(out) - lipgloss.Height(footer)
		if gap > 0 {
			out += strings.Repeat("\n", gap)
		}
	}
	return out + "\n" + footer
}

// panel wraps content in the rounded box, fitting it to the terminal width.
func (m Model) panel(content string) string {
	w, _ := m.dims()
	st := panelStyle
	if w > 8 {
		st = st.Width(w - 4) // account for border (2) + horizontal padding (2)
	}
	return st.Render(content)
}

type keycap struct{ key, desc string }

// footerKeys renders a "key desc · key desc" hint line.
func footerKeys(caps ...keycap) string {
	parts := make([]string, len(caps))
	for i, c := range caps {
		parts[i] = keyStyle.Render(c.key) + " " + keyDescStyle.Render(c.desc)
	}
	return strings.Join(parts, keyDescStyle.Render("  ·  "))
}

// --- matrix --------------------------------------------------------------

// matrixVisibleRows is how many skill rows fit in the table given the terminal
// height. It must agree with viewMatrix's layout so scrolling stays in sync.
func (m Model) matrixVisibleRows() int {
	_, h := m.dims()
	if h <= 0 {
		return len(m.skills) // unbounded: show everything (tests, first frame)
	}
	footerH := lipgloss.Height(m.matrixFooter())
	// header(1) + blank(1) + table chrome top/header/separator/bottom(4) +
	// scroll status line(1) + footer.
	avail := h - 2 - 4 - 1 - footerH
	if avail < 1 {
		avail = 1
	}
	return avail
}

func (m Model) viewMatrix() string {
	status := ""
	switch {
	case m.refreshing:
		status = "⟳ refreshing…"
	case m.applying:
		status = "⟳ applying…"
	}
	header := m.headerBar(status)
	footer := m.matrixFooter()

	if len(m.targets) == 0 {
		return m.frame(header, m.panel(dimStyle.Render("No targets configured. Press ")+
			keyStyle.Render("c")+dimStyle.Render(" to add one.")), footer)
	}
	if len(m.skills) == 0 {
		msg := "No skills found in the configured sources."
		if m.refreshing {
			msg = "Scanning sources…"
		}
		return m.frame(header, m.panel(dimStyle.Render(msg)), footer)
	}

	// A skill name offered by more than one Source is ambiguous; flag the row.
	nameCount := map[string]int{}
	for _, s := range m.skills {
		nameCount[s.Name]++
	}

	vis := m.matrixVisibleRows()
	offset := m.scroll
	end := offset + vis
	if end > len(m.skills) {
		end = len(m.skills)
	}
	visible := m.skills[offset:end]

	headers := make([]string, 0, len(m.targets)+1)
	headers = append(headers, "Skill")
	for _, t := range m.targets {
		headers = append(headers, t.Name)
	}

	// Build the row strings and a parallel grid of per-cell metadata that the
	// StyleFunc closure consults for colouring.
	type cellMeta struct {
		st      domain.Status
		desired bool
	}
	grid := make([][]cellMeta, len(visible))
	rows := make([][]string, len(visible))
	for vi, s := range visible {
		row := make([]string, len(m.targets)+1)
		row[0] = s.Name + " (" + s.Source + ")"
		grid[vi] = make([]cellMeta, len(m.targets))
		for ci, t := range m.targets {
			st := m.status[statusKey{s.Name, s.Source, t.Name}]
			des := m.desired[reconcile.Cell{Skill: s.Name, Source: s.Source, Target: t.Name}]
			grid[vi][ci] = cellMeta{st, des}
			glyph := statusGlyph[st]
			if glyph == "" {
				glyph = "·"
			}
			mark := " "
			if des {
				mark = "✓"
			}
			row[ci+1] = mark + glyph
		}
		rows[vi] = row
	}

	cell := lipgloss.NewStyle().Padding(0, 1)
	tbl := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(tableBorderStyle).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableHeadStyle.Padding(0, 1)
			}
			if col == 0 {
				if nameCount[visible[row].Name] > 1 {
					return cell.Foreground(cRed)
				}
				return cell
			}
			ci := col - 1
			meta := grid[row][ci]
			if offset+row == m.row && ci == m.col {
				return cursorStyle.Padding(0, 1).Align(lipgloss.Center)
			}
			s := cell.Align(lipgloss.Center).Foreground(statusStyles[meta.st].GetForeground())
			if meta.desired {
				s = s.Bold(true)
			}
			return s
		})

	out := tbl.String()
	if w, _ := m.dims(); lipgloss.Width(out) > w {
		out = tbl.Width(w).String()
	}

	scroll := dimStyle.Render(fmt.Sprintf("showing %d–%d of %d", offset+1, end, len(m.skills)))
	if vis < len(m.skills) {
		scroll += dimStyle.Render("  ↑↓ to scroll")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, out, scroll)
	return m.frame(header, body, footer)
}

func (m Model) matrixFooter() string {
	var b strings.Builder
	if len(m.sourceErrors) > 0 {
		names := make([]string, 0, len(m.sourceErrors))
		for n := range m.sourceErrors {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			b.WriteString(errStyle.Render(fmt.Sprintf("⚠ source %q: %v", n, m.sourceErrors[n])) + "\n")
		}
	}
	legend := strings.Join([]string{
		statusStyles[domain.StatusUpToDate].Render("= up-to-date"),
		statusStyles[domain.StatusUpdateAvailable].Render("↑ update"),
		dimStyle.Render("· not-installed"),
		statusStyles[domain.StatusConflict].Render("! conflict"),
		dimStyle.Render("✓ selected"),
	}, dimStyle.Render("   "))
	keys := footerKeys(
		keycap{"↑↓←→", "move"},
		keycap{"space", "toggle"},
		keycap{"a", "all"},
		keycap{"n", "none"},
		keycap{"r", "refresh"},
		keycap{"p", "plan"},
		keycap{"c", "config"},
		keycap{"q", "quit"},
	)
	b.WriteString(legend + "\n" + keys)
	return b.String()
}

// --- plan / overwrite / result ------------------------------------------

func (m Model) viewPlan() string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Plan") + "\n\n")
	if len(m.plan.Operations) == 0 {
		b.WriteString(dimStyle.Render("Nothing to do — selection already matches reality."))
		return m.frame(m.headerBar("plan"), m.panel(b.String()),
			footerKeys(keycap{"any key", "back"}))
	}
	for _, op := range m.plan.Operations {
		line := fmt.Sprintf("%-9s %s", op.Kind, describeOp(op))
		if op.Kind == reconcile.Conflict {
			line = errStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return m.frame(m.headerBar("plan"), m.panel(strings.TrimRight(b.String(), "\n")),
		footerKeys(keycap{"y", "apply"}, keycap{"n", "cancel"}))
}

func describeOp(op reconcile.Operation) string {
	s := op.SkillName
	if op.SourceName != "" {
		s += fmt.Sprintf(" (%s)", op.SourceName)
	}
	s += " → " + op.TargetName
	if op.Reason != "" {
		s += dimStyle.Render("  [" + op.Reason + "]")
	}
	return s
}

func (m Model) viewOverwrite() string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Overwrite untracked folders?") + "\n\n")
	b.WriteString(dimStyle.Render("These folders already exist but were not installed by skillmux:") + "\n\n")
	for _, c := range m.collisions {
		b.WriteString(errStyle.Render(c.SkillName) +
			dimStyle.Render(" ("+c.SourceName+") → "+c.TargetName) + "\n")
		b.WriteString(dimStyle.Render("  "+c.Dir) + "\n")
	}
	return m.frame(m.headerBar("overwrite"), m.panel(strings.TrimRight(b.String(), "\n")),
		footerKeys(keycap{"y", "adopt"}, keycap{"n", "cancel"}))
}

func (m Model) viewResult() string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Result") + "\n\n")
	if m.applyErr != nil {
		b.WriteString(errStyle.Render("persist error: "+m.applyErr.Error()) + "\n\n")
	}
	ok, failed := 0, 0
	for _, r := range m.report.Results {
		if r.OK {
			ok++
			b.WriteString(statusStyles[domain.StatusUpToDate].Render("✓ ") + describeOp(r.Op) + "\n")
		} else {
			failed++
			b.WriteString(errStyle.Render("✗ ") + describeOp(r.Op) + errStyle.Render("  "+r.Err.Error()) + "\n")
		}
	}
	summary := statusStyles[domain.StatusUpToDate].Render(fmt.Sprintf("%d ok", ok))
	if failed > 0 {
		summary += dimStyle.Render(", ") + errStyle.Render(fmt.Sprintf("%d failed", failed))
	}
	b.WriteString("\n" + summary)
	return m.frame(m.headerBar("result"), m.panel(b.String()),
		footerKeys(keycap{"any key", "continue"}, keycap{"q", "quit"}))
}

// --- config / form -------------------------------------------------------

func (m Model) viewConfig() string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Configuration") + "\n\n")

	entries := m.cfgEntries()
	if len(entries) == 0 {
		b.WriteString(dimStyle.Render("No targets or sources yet."))
	}
	for i, e := range entries {
		kind := "target"
		if e.kind == entrySource {
			kind = "source"
		}
		line := fmt.Sprintf("%-7s %-16s %s", kind, e.name, dimStyle.Render(e.detail))
		if i == m.cfgCursor {
			line = cursorStyle.Render(fmt.Sprintf(" %-7s %-16s %s ", kind, e.name, e.detail))
		}
		b.WriteString(line + "\n")
	}
	return m.frame(m.headerBar("config"), m.panel(strings.TrimRight(b.String(), "\n")),
		footerKeys(
			keycap{"t", "add target"},
			keycap{"s", "add source"},
			keycap{"d", "delete"},
			keycap{"↑↓", "move"},
			keycap{"esc", "back"},
		))
}

func (m Model) viewForm() string {
	var b strings.Builder
	b.WriteString(headingStyle.Render(m.form.title) + "\n\n")
	for i, in := range m.form.inputs {
		marker := "  "
		if i == m.form.focus {
			marker = keyStyle.Render("▸ ")
		}
		b.WriteString(marker + dimStyle.Render(pad(m.form.labels[i]+":", 22)) + in.View() + "\n")
	}
	if m.form.err != "" {
		b.WriteString("\n" + errStyle.Render("⚠ "+m.form.err))
	}
	return m.frame(m.headerBar("config"), m.panel(strings.TrimRight(b.String(), "\n")),
		footerKeys(keycap{"tab", "next"}, keycap{"enter", "save"}, keycap{"esc", "cancel"}))
}

// pad right-pads s to width w (ANSI-aware).
func pad(s string, w int) string {
	gap := w - lipgloss.Width(s)
	if gap < 0 {
		gap = 0
	}
	return s + strings.Repeat(" ", gap)
}
