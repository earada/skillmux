package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
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
	case modeSkillTree:
		return m.viewSkillTree()
	case modeFileView:
		return m.viewFileView()
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

// isDeprecated reports whether a skill should be treated as retired: either its
// SKILL.md frontmatter says so, or it lives under a `deprecated` folder. Both
// signals feed the same treatment — bottom section, glyph, struck name.
func isDeprecated(s engine.AvailableSkill) bool {
	return s.Deprecated || strings.Contains(strings.ToLower(s.Group), "deprecated")
}

// skillLabel renders the left-column label for a skill row. The name leads (it
// is the eye-anchor) in an accent colour — or red when conflicting, dimmed and
// struck when deprecated; its folder group trails as a dimmed hint, then the
// Source in parentheses.
func skillLabel(s engine.AvailableSkill, conflict bool) string {
	var name string
	switch {
	case isDeprecated(s):
		name = deprecatedStyle.Render(deprecatedGlyph + " " + s.Name)
	case conflict:
		name = conflictNameStyle.Render(s.Name)
	default:
		name = skillNameStyle.Render(s.Name)
	}
	if s.Group != "" {
		name += groupStyle.Render("  ") + renderGroup(s.Group)
	}
	return name + " (" + s.Source + ")"
}

// renderGroup renders a folder-group hint dimmed, but reddens every occurrence
// of the word "deprecated" (case-insensitive) so a `deprecated/` folder in the
// path jumps out.
func renderGroup(group string) string {
	const word = "deprecated"
	low := strings.ToLower(group)
	var b strings.Builder
	for i := 0; i < len(group); {
		j := strings.Index(low[i:], word)
		if j < 0 {
			b.WriteString(groupStyle.Render(group[i:]))
			break
		}
		j += i
		b.WriteString(groupStyle.Render(group[i:j]))
		b.WriteString(deprecatedWordStyle.Render(group[j : j+len(word)]))
		i = j + len(word)
	}
	return b.String()
}

// dividerMarker is an invisible (zero-width) sentinel placed in a section
// divider's first cell. lipgloss's table can't draw a border rule between two
// chosen rows, so after rendering we swap every line carrying this marker for a
// full-width rule (replaceDividers) — a clean line clear across the grid.
const dividerMarker = "​"

// dividerRow is the placeholder row inserted between two sections; its cells are
// empty (so it never widens a column) save for the invisible marker.
func dividerRow(ntargets int) []string {
	row := make([]string, ntargets+1)
	row[0] = dividerMarker
	return row
}

// replaceDividers swaps each marker-bearing line in a rendered table for the
// table's own header rule (`├───┼───┤`), so dividers span the full width with
// proper column junctions regardless of the final (possibly clamped) widths.
func replaceDividers(out string) string {
	if !strings.Contains(out, dividerMarker) {
		return out
	}
	lines := strings.Split(out, "\n")
	rule := ""
	for _, l := range lines {
		if strings.Contains(l, "├") { // the header/body separator rule
			rule = l
			break
		}
	}
	for i, l := range lines {
		if strings.Contains(l, dividerMarker) {
			lines[i] = rule
		}
	}
	return strings.Join(lines, "\n")
}

// matrixVisibleRows is how many skill rows fit in the table given the terminal
// height. It must agree with viewMatrix's layout so scrolling stays in sync.
func (m Model) matrixVisibleRows() int {
	_, h := m.dims()
	if h <= 0 {
		return len(m.rows()) // unbounded: show everything (tests, first frame)
	}
	footerH := lipgloss.Height(m.matrixFooter())
	// header(1) + blank(1) + table chrome top/header/separator/bottom(4) +
	// scroll status line(1) + cursor detail line(1) + footer. The detail line
	// (deprecation note / needs-suggests) is reserved unconditionally so moving
	// the cursor onto a skill that has one never shoves a row off-screen.
	avail := h - 2 - 4 - 1 - 1 - footerH
	// Reserve room for the section-divider lines so one never shoves a skill
	// row off-screen; they cost a line each on top of the data rows.
	avail -= m.sectionBoundaries()
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

	skills := m.rows()
	if len(skills) == 0 {
		msg := fmt.Sprintf("No skills match %q.", m.filter)
		return m.frame(header, m.panel(dimStyle.Render(msg)), footer)
	}

	// A skill name offered by more than one Source is ambiguous; flag the row.
	// Counted over the full catalogue so the flag is stable under filtering.
	nameCount := map[string]int{}
	for _, s := range m.skills {
		nameCount[s.Name]++
	}

	vis := m.matrixVisibleRows()
	offset := m.scroll
	end := offset + vis
	if end > len(skills) {
		end = len(skills)
	}
	visible := skills[offset:end]

	headers := make([]string, 0, len(m.targets)+1)
	headers = append(headers, "Skill")
	for _, t := range m.targets {
		headers = append(headers, t.Name)
	}

	// A cell turns amber when it is present in its Target but its Dependency
	// closure is unsatisfied there; computed once for the whole matrix.
	broken := m.brokenCells()

	// Build the row strings and a parallel grid of per-cell metadata that the
	// StyleFunc closure consults for colouring.
	type cellMeta struct {
		st      domain.Status
		desired bool
		broken  bool
	}
	grid := make([][]cellMeta, len(visible))
	rows := make([][]string, len(visible))
	for vi, s := range visible {
		row := make([]string, len(m.targets)+1)
		row[0] = skillLabel(s, nameCount[s.Name] > 1)
		grid[vi] = make([]cellMeta, len(m.targets))
		for ci, t := range m.targets {
			rc := reconcile.Cell{Skill: s.Name, Source: s.Source, Target: t.Name}
			st := m.status[statusKey{s.Name, s.Source, t.Name}]
			des := m.desired[rc]
			grid[vi][ci] = cellMeta{st, des, broken[rc]}
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

	// Interleave a divider before each visible row that opens a new section, so
	// the grid reads "installed │ not installed │ deprecated". rowToData maps a
	// table row back to its index in `visible` (-1 for a divider) so the
	// StyleFunc and cursor logic stay aligned despite the inserted rows.
	tableRows := make([][]string, 0, len(visible)+m.sectionBoundaries())
	rowToData := make([]int, 0, cap(tableRows))
	for vi := range visible {
		gi := offset + vi
		if gi > 0 && m.section(skills[gi]) != m.section(skills[gi-1]) {
			tableRows = append(tableRows, dividerRow(len(m.targets)))
			rowToData = append(rowToData, -1)
		}
		tableRows = append(tableRows, rows[vi])
		rowToData = append(rowToData, vi)
	}

	cell := lipgloss.NewStyle().Padding(0, 1)
	tbl := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(tableBorderStyle).
		Headers(headers...).
		Rows(tableRows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableHeadStyle.Padding(0, 1)
			}
			di := rowToData[row]
			if di < 0 { // section divider: content is pre-styled, only space it
				if col == 0 {
					return cell
				}
				return cell.Align(lipgloss.Center)
			}
			if col == 0 {
				// Name colour (accent / conflict-red / deprecated) is baked into
				// the label itself; the cell only supplies padding.
				return cell
			}
			ci := col - 1
			meta := grid[di][ci]
			if offset+di == m.row && ci == m.col {
				return cursorStyle.Padding(0, 1).Align(lipgloss.Center)
			}
			s := cell.Align(lipgloss.Center).Foreground(statusStyles[meta.st].GetForeground())
			if meta.broken {
				s = s.Foreground(cAmber) // problem-first: closure unsatisfied here
			}
			if meta.desired {
				s = s.Bold(true)
			}
			return s
		})

	out := tbl.String()
	if w, _ := m.dims(); lipgloss.Width(out) > w {
		out = tbl.Width(w).String()
	}
	out = replaceDividers(out)

	total := fmt.Sprintf("%d", len(skills))
	if m.filter != "" {
		total = fmt.Sprintf("%d of %d", len(skills), len(m.skills))
	}
	scroll := dimStyle.Render(fmt.Sprintf("showing %d–%d of %s", offset+1, end, total))
	if vis < len(skills) {
		scroll += dimStyle.Render("  ↑↓ to scroll")
	}
	lines := []string{out, scroll}
	// Surface the deprecation note for the skill under the cursor; the glyph in
	// the row only says "deprecated", this says why / what to use instead.
	if cur, ok := m.curSkill(); ok && isDeprecated(cur) {
		note := deprecatedGlyph + " deprecated"
		if cur.DeprecationReason != "" {
			note += ": " + cur.DeprecationReason
		}
		lines = append(lines, dimStyle.Render(note))
	}
	// Below the deprecation note (if any): what the cursor Skill needs / suggests.
	if detail := m.depDetail(); detail != "" {
		lines = append(lines, detail)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
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
		deprecatedStyle.Render(deprecatedGlyph + " deprecated"),
	}, dimStyle.Render("   "))
	// The search line replaces the legend while typing; once a filter is set
	// but the line is dismissed, show a compact reminder of how to edit/clear.
	switch {
	case m.searching:
		b.WriteString(m.search.View() + "\n")
		b.WriteString(footerKeys(keycap{"enter", "apply"}, keycap{"esc", "clear"}))
		return b.String()
	case m.filter != "":
		b.WriteString(keyStyle.Render("/") + dimStyle.Render(" "+m.filter) + "\n")
		b.WriteString(footerKeys(keycap{"/", "edit"}, keycap{"esc", "clear"}, keycap{"q", "quit"}))
		return b.String()
	}

	keys := footerKeys(
		keycap{"↑↓←→", "move"},
		keycap{"space", "toggle"},
		keycap{"a", "all"},
		keycap{"n", "none"},
		keycap{"d", "+deps"},
		keycap{"/", "filter"},
		keycap{"v", "view"},
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
	broken := m.brokenList()

	if len(m.plan.Operations) == 0 {
		b.WriteString(dimStyle.Render("Nothing to do — selection already matches reality."))
	} else {
		lines := make([]string, len(m.plan.Operations))
		for i, op := range m.plan.Operations {
			line := fmt.Sprintf("%-9s %s", op.Kind, describeOp(op))
			if op.Kind == reconcile.Conflict {
				line = errStyle.Render(line)
			}
			lines[i] = line
		}
		b.WriteString(strings.Join(lines, "\n"))
	}

	// The broken section is non-blocking: it warns that the selection leaves an
	// unsatisfied closure, but Apply ('y') still proceeds as-is.
	if len(broken) > 0 {
		b.WriteString("\n\n" + m.renderBrokenSection(broken))
	}

	// Footer: 'y' applies whenever there is work; 'f' offers to add the missing
	// closure when something is fixable; otherwise the empty plan just dismisses.
	var caps []keycap
	if len(m.plan.Operations) > 0 {
		caps = append(caps, keycap{"y", "apply"})
	}
	if fixable(broken) {
		caps = append(caps, keycap{"f", "fix"})
	}
	if len(caps) == 0 {
		caps = append(caps, keycap{"any key", "back"})
	} else {
		caps = append(caps, keycap{"n", "cancel"})
	}
	return m.frame(m.headerBar("plan"), m.panel(strings.TrimRight(b.String(), "\n")),
		footerKeys(caps...))
}

// renderBrokenSection renders the non-blocking "⚠ broken" list: one line per
// present cell with an unsatisfied closure, naming the missing Skills. A
// cross-Source resolution is annotated with the Source that supplies it; a
// dependency no Source offers is marked unresolvable (f cannot fix it).
func (m Model) renderBrokenSection(broken []brokenEntry) string {
	var b strings.Builder
	b.WriteString(brokenStyle.Render("⚠ broken") +
		dimStyle.Render("  — these selections leave dependencies unsatisfied") + "\n")
	for _, e := range broken {
		needs := make([]string, len(e.Missing))
		for i, md := range e.Missing {
			switch {
			case md.Source == "":
				needs[i] = md.Name + errStyle.Render(" (unresolvable)")
			case md.CrossSource:
				needs[i] = md.Name + dimStyle.Render(" ("+md.Source+")")
			default:
				needs[i] = md.Name
			}
		}
		head := brokenStyle.Render(e.Cell.Skill) +
			dimStyle.Render(" ("+e.Cell.Source+") → "+e.Cell.Target+"  needs ")
		b.WriteString("  " + head + strings.Join(needs, dimStyle.Render(", ")) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
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
	w, _ := m.dims()
	for i, e := range entries {
		// Rule a line where Sources give way to Targets, mirroring the matrix.
		if i > 0 && e.kind != entries[i-1].kind {
			b.WriteString(dimStyle.Render(strings.Repeat("─", max(0, w-6))) + "\n")
		}
		kind := "target"
		detail := e.detail
		if e.kind == entrySource {
			kind = "source"
			if rev, ok := m.cat.Revisions[e.name]; ok {
				detail += "  " + rev.Label()
				if !rev.FetchedAt.IsZero() {
					detail += " · fetched " + humanizeSince(rev.FetchedAt)
				}
			}
		}
		line := fmt.Sprintf("%-7s %-16s %s", kind, e.name, dimStyle.Render(detail))
		if i == m.cfgCursor {
			line = cursorStyle.Render(fmt.Sprintf(" %-7s %-16s %s ", kind, e.name, detail))
		}
		b.WriteString(line + "\n")
	}
	if m.cfgMsg != "" {
		b.WriteString("\n" + dimStyle.Render(m.cfgMsg) + "\n")
	}
	return m.frame(m.headerBar("config"), m.panel(strings.TrimRight(b.String(), "\n")),
		footerKeys(
			keycap{"t", "add target"},
			keycap{"s", "add source"},
			keycap{"e", "edit"},
			keycap{"d", "delete"},
			keycap{"C", "clear cache"},
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

// --- skill view (tree / file) -------------------------------------------

func (m Model) viewSkillTree() string {
	header := m.headerBar("view")
	footer := m.skillTreeFooter()

	lines := append([]string(nil), m.treeMetaLines()...)
	w, _ := m.dims()
	lines = append(lines, dimStyle.Render(strings.Repeat("─", max(0, w-2))))

	nEdges := len(m.viewEdges)
	if nEdges > 0 {
		lines = append(lines, headingStyle.Render("Dependencies"))
		for i, e := range m.viewEdges {
			lines = append(lines, m.edgeRow(e, i == m.treeCursor))
		}
		lines = append(lines, "", headingStyle.Render("Files"))
	}

	switch {
	case !m.treeOK:
		lines = append(lines, dimStyle.Render("(files unavailable — not downloaded yet)"))
	case len(m.viewTree) == 0:
		lines = append(lines, dimStyle.Render("(empty)"))
	default:
		vis := m.treeVisibleRows()
		end := min(m.treeScroll+vis, len(m.viewTree))
		for i := m.treeScroll; i < end; i++ {
			lines = append(lines, m.treeRow(m.viewTree[i], nEdges+i == m.treeCursor))
		}
		if vis < len(m.viewTree) {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("showing %d–%d of %d  ↑↓ to scroll",
				m.treeScroll+1, end, len(m.viewTree))))
		}
	}
	if m.viewMsg != "" {
		lines = append(lines, dimStyle.Render(m.viewMsg))
	}
	return m.frame(header, lipgloss.JoinVertical(lipgloss.Left, lines...), footer)
}

// edgeRow renders one outgoing-edge line in the Dependencies section: the target
// Skill name, whether it is a Dependency or a Suggestion, the resolving Source
// when it crosses Sources, and a note when a router-wide entry holds it down.
// Suggestions render dimmed — never the warning colour — distinct from a
// Dependency, per ADR 0005.
func (m Model) edgeRow(e skillEdge, cursor bool) string {
	label := e.to
	if e.crossSource && e.source != "" {
		label += " (" + e.source + ")"
	}
	kind := "dependency"
	if e.suggestion {
		kind = "suggestion"
	}
	note := ""
	if e.bulk {
		note = " · router-wide (edit config.toml)"
	}
	if cursor {
		return cursorStyle.Render(" " + label + "  —  " + kind + note + " ")
	}
	nameStyle := skillNameStyle
	if e.suggestion {
		nameStyle = dimStyle
	}
	return "  " + nameStyle.Render(label) + dimStyle.Render("  —  "+kind+note)
}

// treeRow renders one file-tree line: indented by depth, directories suffixed
// with "/", the cursor row highlighted.
func (m Model) treeRow(t treeLine, cursor bool) string {
	name := t.name
	if t.isDir {
		name += "/"
	}
	label := strings.Repeat("  ", t.depth) + name
	if cursor {
		return cursorStyle.Render(" " + label + " ")
	}
	if t.isDir {
		return dimStyle.Render("  " + label)
	}
	return "  " + label
}

func (m Model) skillTreeFooter() string {
	caps := []keycap{{"↑↓", "move"}}
	if _, ok := m.curEdge(); ok {
		caps = append(caps, keycap{"t", "dep⇄suggest"})
	} else {
		caps = append(caps, keycap{"enter", "open"})
	}
	caps = append(caps, keycap{"esc", "back"}, keycap{"q", "quit"})
	return footerKeys(caps...)
}

func (m Model) viewFileView() string {
	crumb := skillNameStyle.Render(m.viewSkill.Name) + dimStyle.Render(" / "+m.openPath)
	inner := m.fileVP.View()
	if m.fileLoading {
		inner = dimStyle.Render("loading…")
	}
	body := crumb + "\n" + inner
	return m.frame(m.headerBar("view"), body, m.fileFooter())
}

func (m Model) fileFooter() string {
	return footerKeys(
		keycap{"↑↓", "scroll"},
		keycap{"esc", "back"},
		keycap{"q", "quit"},
	)
}

// humanizeSince renders how long ago t was as a compact relative string
// ("just now", "5m ago", "3h ago", "2d ago"), for the Sources list.
func humanizeSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// pad right-pads s to width w (ANSI-aware).
func pad(s string, w int) string {
	gap := w - lipgloss.Width(s)
	if gap < 0 {
		gap = 0
	}
	return s + strings.Repeat(" ", gap)
}
