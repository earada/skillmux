package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/earada/skillmux/internal/config"
)

// cfgEntryKind distinguishes Target rows from Source rows in the config list.
type cfgEntryKind int

const (
	entryTarget cfgEntryKind = iota
	entrySource
)

type cfgEntry struct {
	kind   cfgEntryKind
	name   string
	detail string
}

// cfgEntries flattens the configured Sources then Targets for display, grouped
// the way the matrix groups skills: Sources on top, Targets below a divider.
func (m Model) cfgEntries() []cfgEntry {
	var out []cfgEntry
	for _, s := range m.eng.Config.Sources {
		out = append(out, cfgEntry{entrySource, s.Name, s.Location})
	}
	for _, t := range m.eng.Config.Targets {
		out = append(out, cfgEntry{entryTarget, t.Name, t.Path})
	}
	return out
}

func findTarget(ts []config.TargetEntry, name string) (config.TargetEntry, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	return config.TargetEntry{}, false
}

func findSource(ss []config.SourceEntry, name string) (config.SourceEntry, bool) {
	for _, s := range ss {
		if s.Name == name {
			return s, true
		}
	}
	return config.SourceEntry{}, false
}

func (m Model) onConfigKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.cfgEntries()
	switch msg.String() {
	case "q", "esc":
		return m.leaveConfig()
	case "up", "k":
		m.cfgCursor--
	case "down", "j":
		m.cfgCursor++
	case "t":
		m.form = newTargetForm()
		m.mode = modeForm
		return m, nil
	case "s":
		m.form = newSourceForm()
		m.mode = modeForm
		return m, nil
	case "e":
		if m.cfgCursor >= 0 && m.cfgCursor < len(entries) {
			e := entries[m.cfgCursor]
			if e.kind == entryTarget {
				if t, ok := findTarget(m.eng.Config.Targets, e.name); ok {
					m.form = editTargetForm(t)
					m.mode = modeForm
				}
			} else if s, ok := findSource(m.eng.Config.Sources, e.name); ok {
				m.form = editSourceForm(s)
				m.mode = modeForm
			}
		}
		return m, nil
	case "d", "x":
		if m.cfgCursor >= 0 && m.cfgCursor < len(entries) {
			e := entries[m.cfgCursor]
			if e.kind == entryTarget {
				_ = m.eng.RemoveTarget(e.name)
			} else {
				_ = m.eng.RemoveSource(e.name)
			}
		}
	case "C":
		if m.cfgCursor >= 0 && m.cfgCursor < len(entries) {
			e := entries[m.cfgCursor]
			if e.kind == entrySource {
				cached, err := m.eng.ClearSourceCache(e.name)
				m.cfgMsg = clearCacheResult(cached, err, e.name)
			}
		}
	}
	m.cfgCursor = clamp(m.cfgCursor, 0, len(m.cfgEntries())-1)
	return m, nil
}

// clearCacheResult turns the outcome of Engine.ClearSourceCache into the status
// line shown in the config view.
func clearCacheResult(cached bool, err error, name string) string {
	switch {
	case err != nil:
		return "cache: " + err.Error()
	case cached:
		return "Cleared cache for " + name + " — refresh to re-download."
	default:
		return name + " has no cache (local source)."
	}
}

// leaveConfig returns to the matrix, picking up any Target changes and
// re-scanning Sources so the matrix reflects the new Config.
func (m Model) leaveConfig() (tea.Model, tea.Cmd) {
	m.targets = m.eng.Config.DomainTargets()
	m.mode = modeMatrix
	m.clampCursor()
	// Coalesce: if a Refresh is already in flight (e.g. the startup one), queue
	// the re-scan instead of launching a second, concurrent Refresh (skillmux-3vj).
	m, cmd := m.requestRefresh()
	return m, cmd
}

func (m Model) onFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.form = nil
		m.mode = modeConfig
		return m, nil
	case "tab", "down":
		m.form.focusNext()
		return m, nil
	case "shift+tab", "up":
		m.form.focusPrev()
		return m, nil
	case "enter":
		if err := m.submitForm(); err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		m.form = nil
		m.mode = modeConfig
		m.cfgCursor = clamp(m.cfgCursor, 0, len(m.cfgEntries())-1)
		return m, nil
	}
	// Route any other key to the focused text input.
	var cmd tea.Cmd
	m.form.inputs[m.form.focus], cmd = m.form.inputs[m.form.focus].Update(msg)
	return m, cmd
}

func (m *Model) submitForm() error {
	v := m.form.values()
	switch m.form.kind {
	case formTarget:
		if m.form.editing {
			return m.eng.UpdateTarget(m.form.origName, v[0], v[1])
		}
		return m.eng.AddTarget(v[0], v[1])
	default:
		s := config.SourceEntry{Name: v[0], Location: v[1], Branch: v[2], Subpath: v[3]}
		if m.form.editing {
			return m.eng.UpdateSource(m.form.origName, s)
		}
		return m.eng.AddSource(s)
	}
}

// --- form ---

type formKind int

const (
	formTarget formKind = iota
	formSource
)

type configForm struct {
	kind     formKind
	title    string
	labels   []string
	inputs   []textinput.Model
	focus    int
	err      string
	editing  bool   // true when editing an existing entry rather than adding
	origName string // the entry's name before editing, for the in-place update
}

func newTargetForm() *configForm {
	return newForm(formTarget, "Add target",
		[]string{"name", "path"},
		[]string{"claude-code", "~/.claude/skills"})
}

func newSourceForm() *configForm {
	return newForm(formSource, "Add source",
		[]string{"name", "location", "branch (optional)", "subpath (optional)"},
		[]string{"my-skills", "https://github.com/owner/repo or ~/dev/skills", "main", "skills"})
}

// editTargetForm / editSourceForm build a form pre-filled with an existing
// entry's values; submitForm then updates it in place rather than adding.
func editTargetForm(t config.TargetEntry) *configForm {
	f := newTargetForm()
	f.title, f.editing, f.origName = "Edit target", true, t.Name
	f.setValues(t.Name, t.Path)
	return f
}

func editSourceForm(s config.SourceEntry) *configForm {
	f := newSourceForm()
	f.title, f.editing, f.origName = "Edit source", true, s.Name
	f.setValues(s.Name, s.Location, s.Branch, s.Subpath)
	return f
}

func newForm(kind formKind, title string, labels, placeholders []string) *configForm {
	inputs := make([]textinput.Model, len(labels))
	for i := range labels {
		ti := textinput.New()
		ti.Placeholder = placeholders[i]
		if i == 0 {
			ti.Focus()
		}
		inputs[i] = ti
	}
	return &configForm{kind: kind, title: title, labels: labels, inputs: inputs}
}

func (f *configForm) focusNext() { f.setFocus((f.focus + 1) % len(f.inputs)) }
func (f *configForm) focusPrev() { f.setFocus((f.focus - 1 + len(f.inputs)) % len(f.inputs)) }

func (f *configForm) setFocus(i int) {
	f.inputs[f.focus].Blur()
	f.focus = i
	f.inputs[f.focus].Focus()
}

// setValues pre-fills the inputs (used when editing an existing entry).
func (f *configForm) setValues(vals ...string) {
	for i, v := range vals {
		if i < len(f.inputs) {
			f.inputs[i].SetValue(v)
		}
	}
}

func (f *configForm) values() []string {
	out := make([]string, len(f.inputs))
	for i, in := range f.inputs {
		out[i] = strings.TrimSpace(in.Value())
	}
	return out
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
