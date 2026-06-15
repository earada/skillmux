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
	modeResult
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
	report     apply.Report
	applyErr   error

	width, height int
}

// New builds the initial Model for engine e.
func New(e *engine.Engine) Model {
	return Model{
		eng:          e,
		targets:      e.Config.DomainTargets(),
		status:       map[statusKey]domain.Status{},
		desired:      map[reconcile.Cell]bool{},
		sourceErrors: map[string]error{},
	}
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

func applyCmd(e *engine.Engine, desired []reconcile.Cell, cat engine.Catalog) tea.Cmd {
	return func() tea.Msg {
		rep, err := e.Apply(desired, cat, apply.Options{})
		return applyDoneMsg{rep: rep, err: err}
	}
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
	case modeResult:
		return m.onResultKey(msg)
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
			setRow(m.desired, sk.Name, sk.Source, m.targets, true)
		}
	case "n":
		if sk, ok := m.curSkill(); ok {
			setRow(m.desired, sk.Name, sk.Source, m.targets, false)
		}
	case "r":
		if !m.refreshing {
			m.refreshing = true
			return m, refreshCmd(m.eng)
		}
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
		m.applying = true
		m.mode = modeMatrix
		return m, applyCmd(m.eng, selected(m.desired), m.cat)
	case "n", "esc", "q":
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
	m.desired[c] = !m.desired[c]
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
