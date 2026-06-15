// Package tui is the Bubble Tea front-end: a Skills × Targets matrix where the
// user edits the desired selection, previews the reconciliation Plan, and
// applies it. All domain work is delegated to the engine. See CONTEXT.md.
package tui

import (
	"sort"

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
)

type statusKey struct{ skill, source, target string }

// Model is the Bubble Tea model for Skillmux.
type Model struct {
	eng     *engine.Engine
	targets []domain.Target

	skills       []engine.AvailableSkill
	status       map[statusKey]domain.Status
	desired      map[reconcile.Cell]bool
	sourceErrors map[string]error
	cat          engine.Catalog
	loaded       bool

	row, col   int
	refreshing bool
	applying   bool
	mode       viewMode
	plan       reconcile.Plan
	collisions []engine.Collision
	report     apply.Report
	applyErr   error

	cfgCursor int         // cursor in the config-management list
	form      *configForm // active add form, when mode == modeForm

	width, height int
}

// New builds the initial Model for engine e. It renders immediately from the
// last cached catalog (if any) so startup is instant; Init then refreshes in
// the background to pick up upstream changes.
func New(e *engine.Engine) Model {
	m := Model{
		eng:          e,
		targets:      e.Config.DomainTargets(),
		status:       map[statusKey]domain.Status{},
		desired:      map[reconcile.Cell]bool{},
		sourceErrors: map[string]error{},
	}
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

func applyCmd(e *engine.Engine, desired []reconcile.Cell, cat engine.Catalog, opts apply.Options) tea.Cmd {
	return func() tea.Msg {
		rep, err := e.Apply(desired, cat, opts)
		return applyDoneMsg{rep: rep, err: err}
	}
}

// approveOverwrites builds a ConfirmOverwrite that approves exactly the
// collisions the user just confirmed, keyed by (Target, Skill).
func approveOverwrites(cols []engine.Collision) func(target, skill, dir string) bool {
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

// Update handles messages. It is split by message type; key handling further
// dispatches on the current view mode.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case refreshDoneMsg:
		return m.onRefreshed(msg.cat), nil

	case applyDoneMsg:
		m.applying = false
		m.report = msg.rep
		m.applyErr = msg.err
		m.mode = modeResult
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

	skills := append([]engine.AvailableSkill(nil), cat.Skills...)
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name != skills[j].Name {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Source < skills[j].Source
	})
	m.skills = skills

	cells := m.eng.Status(cat)
	m.status = map[statusKey]domain.Status{}
	for _, c := range cells {
		m.status[statusKey{c.SkillName, c.SourceName, c.TargetName}] = c.Status
	}
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
	default:
		return m.onMatrixKey(msg)
	}
}

func (m Model) onMatrixKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.row--
	case "down", "j":
		m.row++
	case "left", "h":
		m.col--
	case "right", "l":
		m.col++
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
	case "r":
		if !m.refreshing {
			m.refreshing = true
			return m, refreshCmd(m.eng)
		}
	case "c":
		m.cfgCursor = 0
		m.mode = modeConfig
	case "p", "enter":
		m.plan = m.eng.Plan(selected(m.desired), m.cat)
		m.mode = modePlan
	}
	m.clampCursor()
	return m, nil
}

func (m Model) onPlanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if len(m.plan.Operations) == 0 {
			m.mode = modeMatrix
			return m, nil
		}
		// If any Install would overwrite an untracked folder, confirm first
		// (ADR 0002). Otherwise apply straight away.
		if cols := m.eng.Collisions(m.plan); len(cols) > 0 {
			m.collisions = cols
			m.mode = modeOverwrite
			return m, nil
		}
		m.applying = true
		m.mode = modeMatrix
		return m, applyCmd(m.eng, selected(m.desired), m.cat, apply.Options{})
	case "n", "esc", "q":
		m.mode = modeMatrix
	}
	return m, nil
}

func (m Model) onOverwriteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		opts := apply.Options{ConfirmOverwrite: approveOverwrites(m.collisions)}
		m.collisions = nil
		m.applying = true
		m.mode = modeMatrix
		return m, applyCmd(m.eng, selected(m.desired), m.cat, opts)
	case "n", "esc", "q":
		// Cancel: nothing is touched. Back to the matrix.
		m.collisions = nil
		m.mode = modeMatrix
	}
	return m, nil
}

func (m Model) onResultKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	default:
		// Dismiss results and refresh so statuses reflect what changed.
		m.mode = modeMatrix
		m.refreshing = true
		return m, refreshCmd(m.eng)
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
	if m.row < 0 || m.row >= len(m.skills) {
		return engine.AvailableSkill{}, false
	}
	return m.skills[m.row], true
}

func (m *Model) clampCursor() {
	if m.row < 0 {
		m.row = 0
	}
	if m.row >= len(m.skills) {
		m.row = max(0, len(m.skills)-1)
	}
	if m.col < 0 {
		m.col = 0
	}
	if m.col >= len(m.targets) {
		m.col = max(0, len(m.targets)-1)
	}
}
