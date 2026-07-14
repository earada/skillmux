// Package tui is the Bubble Tea front-end: a Skills × Targets matrix where the
// user edits the desired selection, previews the reconciliation Plan, and
// applies it. All domain work is delegated to the engine. See CONTEXT.md.
package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

type viewMode int

const (
	modeMatrix viewMode = iota
	modePlan
	modeOverwrite
	modeResult
	modeConfig
	modeForm
	modeSkillTree // read-only explorer: metadata + file tree of a Skill
	modeFileView  // scrollable viewer for one file within a Skill
)

type statusKey struct{ skill, source, target string }

// skillRef identifies a matrix row: a Skill name within a specific Source.
type skillRef struct{ name, source string }

// Model is the Bubble Tea model for Skillmux.
type Model struct {
	eng     *engine.Engine
	targets []domain.Target

	skills       []engine.AvailableSkill
	status       map[statusKey]domain.Status
	installed    map[skillRef]bool // skills present in at least one Target
	desired      map[reconcile.Cell]bool
	sourceErrors map[string]error
	cat          engine.Catalog
	// graph answers every dependency question the matrix asks. Built once per
	// Catalog (in onRefreshed) and after a config edit (toggleEdge); queried with
	// the current desired selection per keystroke.
	graph  *engine.SkillGraph
	loaded bool

	row, col       int
	scroll         int // index of the first visible skill row (vertical scroll)
	refreshing     bool
	pendingRefresh bool // a Refresh is wanted but one is already running; run it next
	applying       bool
	mode           viewMode
	// preview is the engine's "what will happen" for the current selection —
	// Plan + Collisions computed once when the Plan screen opens, then executed
	// verbatim by Apply. Replaces separate plan/collisions state.
	preview  engine.Preview
	report   apply.Report
	applyErr error

	cfgCursor int         // cursor in the config-management list
	cfgMsg    string      // transient status line for the config view (e.g. cache cleared)
	form      *configForm // active add form, when mode == modeForm

	search    textinput.Model // the "/" search line
	searching bool            // true while the search line is capturing input
	filter    string          // active filter query; rows() narrows to matches

	// Skill-view state (modeSkillTree / modeFileView).
	viewSkill   engine.AvailableSkill // the Skill being explored
	viewEdges   []skillEdge           // its outgoing Dependency / Suggestion edges
	viewTree    []treeLine            // its recursive file tree
	treeOK      bool                  // false when the folder is missing on disk
	treeCursor  int                   // cursor over the edges-then-files nav list
	treeScroll  int                   // first visible file row (vertical scroll)
	viewMsg     string                // transient note for the skill view (e.g. toggled)
	openPath    string                // relative path of the open file (breadcrumb)
	fileLoading bool                  // true while the open file reads+renders off-loop
	fileContent fileContent           // the classified open file
	fileVP      viewport.Model        // scroll container for the open file

	width, height int
}

// New builds the initial Model for engine e. It renders immediately from the
// last cached catalog (if any) so startup is instant; Init then refreshes in
// the background to pick up upstream changes.
func New(e *engine.Engine) Model {
	search := textinput.New()
	search.Prompt = "/"
	search.Placeholder = "filter skills…"
	m := Model{
		eng:          e,
		targets:      e.Config.DomainTargets(),
		status:       map[statusKey]domain.Status{},
		desired:      map[reconcile.Cell]bool{},
		sourceErrors: map[string]error{},
		search:       search,
	}
	m.graph = e.SkillGraph(engine.Catalog{}) // empty until the first Refresh lands
	if cached := e.CachedCatalog(); len(cached.Skills) > 0 {
		m = m.onRefreshed(cached)
	}
	m.refreshing = true // Init() kicks a background Refresh
	return m
}

// Run starts the TUI program.
func Run(e *engine.Engine) error {
	_, err := tea.NewProgram(New(e), tea.WithAltScreen()).Run()
	return err
}

type refreshDoneMsg struct{ cat engine.Catalog }
type applyDoneMsg struct {
	rep apply.Report
	err error
}

func refreshCmd(e *engine.Engine) tea.Cmd {
	return func() tea.Msg { return refreshDoneMsg{cat: e.Refresh()} }
}

func applyCmd(e *engine.Engine, pre engine.Preview, opts apply.Options) tea.Cmd {
	return func() tea.Msg {
		rep, err := e.Apply(pre, opts)
		return applyDoneMsg{rep: rep, err: err}
	}
}

// approveOverwrites builds a ConfirmOverwrite that approves exactly the
// collisions the user just confirmed, keyed by (Target, Skill).
func approveOverwrites(cols []apply.Collision) func(target, skill, dir string) bool {
	type key struct{ target, skill string }
	approved := map[key]bool{}
	for _, c := range cols {
		approved[key{c.TargetName, c.SkillName}] = true
	}
	return func(target, skill, _ string) bool { return approved[key{target, skill}] }
}

// Init kicks off the first background Refresh.
func (m Model) Init() tea.Cmd {
	return refreshCmd(m.eng)
}

// busy reports whether a filesystem-mutating Engine command — a Refresh or an
// Apply — is currently in flight. While busy the matrix refuses to open the
// Plan, enter config, or start another command, so at most one such command
// runs at a time and none starts from in-flight state (skillmux-3vj).
func (m Model) busy() bool { return m.refreshing || m.applying }

// requestRefresh starts a Refresh, or — when a command is already in flight —
// records that one is wanted so refreshDoneMsg/applyDoneMsg runs it once the
// slot frees. This coalesces a burst of refresh requests (startup refresh +
// config exit, a repeated 'r', a deferred skill-view checkout) into at most one
// queued rerun rather than launching concurrent Refreshes.
func (m Model) requestRefresh() (Model, tea.Cmd) {
	if m.busy() {
		m.pendingRefresh = true
		return m, nil
	}
	m.refreshing = true
	return m, refreshCmd(m.eng)
}

// Update handles messages. It is split by message type; key handling further
// dispatches on the current view mode.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor() // re-window the matrix for the new height
		return m, nil

	case refreshDoneMsg:
		m = m.onRefreshed(msg.cat)
		if m.pendingRefresh {
			// A Refresh was requested while this one ran (e.g. a skill view closed
			// with a deferred checkout). Run it now that the slot is free.
			m.pendingRefresh = false
			m.refreshing = true
			return m, refreshCmd(m.eng)
		}
		return m, nil

	case fileRenderedMsg:
		return m.onFileRendered(msg), nil

	case applyDoneMsg:
		m.applying = false
		m.report = msg.rep
		m.applyErr = msg.err
		m.mode = modeResult
		if m.pendingRefresh {
			// A Refresh was requested while the Apply ran; run it now the slot is
			// free so the result screen's next refresh isn't lost.
			m.pendingRefresh = false
			m.refreshing = true
			return m, refreshCmd(m.eng)
		}
		return m, nil

	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m Model) onRefreshed(cat engine.Catalog) Model {
	m.refreshing = false
	m.cat = cat
	m.sourceErrors = cat.SourceErrors
	m.graph = m.eng.SkillGraph(cat) // rebuild the dependency graph once per Catalog

	// Compute status first: the row order depends on which skills are installed.
	cells := m.eng.Status(cat)
	m.status = map[statusKey]domain.Status{}
	m.installed = map[skillRef]bool{} // present in at least one Target
	for _, c := range cells {
		m.status[statusKey{c.SkillName, c.SourceName, c.TargetName}] = c.Status
		switch c.Status {
		case domain.StatusUpToDate, domain.StatusUpdateAvailable, domain.StatusUnavailable:
			m.installed[skillRef{c.SkillName, c.SourceName}] = true
		}
	}

	// A Skill removed upstream after install has no catalog row but is still in
	// the Manifest; append its last-known row so it stays visible and the user
	// can keep or uninstall it (skillmux-crl).
	skills := append([]engine.AvailableSkill(nil), cat.Skills...)
	skills = append(skills, m.eng.UnavailableSkills(cat)...)
	sort.Slice(skills, func(i, j int) bool {
		// Group into sections — installed, then not-installed, then deprecated
		// — so the matrix can rule a line between each. Within a section, keep
		// each Source's Skills together, alphabetical by name.
		if si, sj := m.section(skills[i]), m.section(skills[j]); si != sj {
			return si < sj
		}
		if skills[i].Source != skills[j].Source {
			return skills[i].Source < skills[j].Source
		}
		return skills[i].Name < skills[j].Name
	})
	m.skills = skills

	if !m.loaded {
		m.desired = initialDesired(cells)
		m.loaded = true
	}
	m.clampCursor()
	return m
}

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modePlan:
		return m.onPlanKey(msg)
	case modeOverwrite:
		return m.onOverwriteKey(msg)
	case modeResult:
		return m.onResultKey(msg)
	case modeConfig:
		return m.onConfigKey(msg)
	case modeForm:
		return m.onFormKey(msg)
	case modeSkillTree:
		return m.onSkillTreeKey(msg)
	case modeFileView:
		return m.onFileViewKey(msg)
	default:
		return m.onMatrixKey(msg)
	}
}

func (m Model) onMatrixKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		return m.onSearchKey(msg)
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "/":
		m.searching = true
		m.search.SetValue(m.filter)
		m.search.CursorEnd()
		m.search.Focus()
		return m, nil
	case "esc":
		if m.filter != "" {
			m.clearFilter()
		}
	case "up", "k":
		m.row--
	case "down", "j":
		m.row++
	case "left", "h":
		m.col--
	case "right", "l":
		m.col++
	case "pgup":
		m.row -= m.matrixVisibleRows()
	case "pgdown":
		m.row += m.matrixVisibleRows()
	case "home", "g":
		m.row = 0
	case "end", "G":
		m.row = len(m.rows()) - 1
	case " ":
		m.toggleCurrent()
	case "a":
		if sk, ok := m.curSkill(); ok {
			setRow(m.desired, sk.Name, sk.Source, m.targets, m.skills, true)
		}
	case "n":
		if sk, ok := m.curSkill(); ok {
			setRow(m.desired, sk.Name, sk.Source, m.targets, m.skills, false)
		}
	case "d":
		m.markClosure()
	case "r":
		return m.requestRefresh()
	case "c":
		// A config edit races a running Refresh (it rewrites Config while Refresh
		// scans it), so config is reachable only when no command is in flight.
		if !m.busy() {
			m.cfgCursor = 0
			m.cfgMsg = ""
			m.mode = modeConfig
		}
	case "v":
		return m.enterSkillView(), nil
	case "p", "enter":
		// Don't open the Plan (and thus a possible Apply) off in-flight state: a
		// running Refresh is rewriting the catalog and a running Apply the
		// Manifest, either of which would make the previewed Plan stale.
		if !m.busy() {
			m.preview = m.eng.Preview(selected(m.desired), m.cat)
			m.mode = modePlan
		}
	}
	m.clampCursor()
	return m, nil
}

// onSearchKey drives the "/" search line. Typing filters the matrix live (vim
// incremental search): Enter keeps the filter and returns to navigation, Esc
// abandons the search and restores the full list.
func (m Model) onSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		m.search.Blur()
		return m, nil
	case "esc":
		m.searching = false
		m.clearFilter()
		return m, nil
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	// Live filter: re-narrow and snap the cursor to the top of the results.
	m.filter = m.search.Value()
	m.row, m.scroll = 0, 0
	m.clampCursor()
	return m, cmd
}

// clearFilter drops the active filter and resets the search line.
func (m *Model) clearFilter() {
	m.filter = ""
	m.search.SetValue("")
	m.search.Blur()
	m.row, m.scroll = 0, 0
	m.clampCursor()
}

func (m Model) onPlanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.applying {
			return m, nil // an Apply is already in flight; never start a second
		}
		if len(m.preview.Plan.Operations) == 0 {
			m.mode = modeMatrix
			return m, nil
		}
		// If any operation would overwrite an untracked folder, confirm first
		// (ADR 0002). The Preview already computed the collisions. Otherwise apply
		// the previewed Plan straight away.
		if len(m.preview.Collisions) > 0 {
			m.mode = modeOverwrite
			return m, nil
		}
		m.applying = true
		m.mode = modeMatrix
		return m, applyCmd(m.eng, m.preview, apply.Options{})
	case "f":
		// Add the resolvable missing closure to the selection and recompute the
		// Preview in place, so the broken section shrinks and new Installs appear.
		m.fixBroken()
		m.preview = m.eng.Preview(selected(m.desired), m.cat)
		return m, nil
	case "n", "esc", "q":
		m.mode = modeMatrix
	}
	return m, nil
}

func (m Model) onOverwriteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if m.applying {
			return m, nil // an Apply is already in flight; never start a second
		}
		opts := apply.Options{ConfirmOverwrite: approveOverwrites(m.preview.Collisions)}
		m.applying = true
		m.mode = modeMatrix
		return m, applyCmd(m.eng, m.preview, opts)
	case "n", "esc", "q":
		// Cancel: nothing is touched. Back to the matrix.
		m.mode = modeMatrix
	}
	return m, nil
}

func (m Model) onResultKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	default:
		// Dismiss results and refresh so statuses reflect what changed, coalescing
		// with any refresh already queued behind the just-finished Apply.
		m.mode = modeMatrix
		m, cmd := m.requestRefresh()
		return m, cmd
	}
}

func (m *Model) toggleCurrent() {
	sk, ok := m.curSkill()
	if !ok || m.col < 0 || m.col >= len(m.targets) {
		return
	}
	c := reconcile.Cell{Skill: sk.Name, Source: sk.Source, Target: m.targets[m.col].Name}
	if m.desired[c] {
		m.desired[c] = false
	} else {
		selectCell(m.desired, c, m.skills) // stay conflict-free
	}
}

func (m *Model) curSkill() (engine.AvailableSkill, bool) {
	rows := m.rows()
	if m.row < 0 || m.row >= len(rows) {
		return engine.AvailableSkill{}, false
	}
	return rows[m.row], true
}

// rows is the list of skills currently shown in the matrix: all of them, or
// just those matching the active filter (case-insensitive substring over the
// skill name, its group, and its source). The cursor (m.row) and scroll index
// always refer to this filtered list, never to m.skills directly.
func (m Model) rows() []engine.AvailableSkill {
	if m.filter == "" {
		return m.skills
	}
	q := strings.ToLower(m.filter)
	out := make([]engine.AvailableSkill, 0, len(m.skills))
	for _, s := range m.skills {
		if strings.Contains(strings.ToLower(s.Group+" "+s.Name+" "+s.Source), q) {
			out = append(out, s)
		}
	}
	return out
}

// Matrix sections, in display order. A ruled line separates each from the next.
const (
	secInstalled    = 0 // present in at least one Target
	secNotInstalled = 1
	secDeprecated   = 2 // author-retired; sinks below everything
)

// section assigns a skill to its matrix section. Deprecated wins regardless of
// install state, so retired skills always gather at the bottom.
func (m Model) section(s engine.AvailableSkill) int {
	switch {
	case isDeprecated(s):
		return secDeprecated
	case m.installed[skillRef{s.Name, s.Source}]:
		return secInstalled
	default:
		return secNotInstalled
	}
}

// sectionBoundaries counts the section transitions among the currently shown
// rows — i.e. how many separator lines the matrix may draw. viewMatrix reserves
// this many lines so a separator never pushes a row off-screen.
func (m Model) sectionBoundaries() int {
	rows := m.rows()
	n := 0
	for i := 1; i < len(rows); i++ {
		if m.section(rows[i]) != m.section(rows[i-1]) {
			n++
		}
	}
	return n
}

func (m *Model) clampCursor() {
	n := len(m.rows())
	if m.row < 0 {
		m.row = 0
	}
	if m.row >= n {
		m.row = max(0, n-1)
	}
	if m.col < 0 {
		m.col = 0
	}
	if m.col >= len(m.targets) {
		m.col = max(0, len(m.targets)-1)
	}

	// Keep the cursor row inside the visible scroll window.
	vis := m.matrixVisibleRows()
	if m.row < m.scroll {
		m.scroll = m.row
	}
	if m.row >= m.scroll+vis {
		m.scroll = m.row - vis + 1
	}
	if hi := n - vis; m.scroll > hi {
		m.scroll = hi
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}
