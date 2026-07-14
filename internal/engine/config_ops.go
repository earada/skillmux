package engine

import (
	"errors"
	"fmt"

	"github.com/earada/skillmux/internal/config"
)

// errNoConfigPath is returned when a mutation is attempted but the Engine was
// built without a Config path to persist to.
var errNoConfigPath = errors.New("no config path configured; cannot persist changes")

// AddTarget appends a Target and persists the Config. Validation (duplicate or
// empty name/path) is enforced by config.Save; on failure the in-memory Config
// is rolled back and nothing is written.
func (e *Engine) AddTarget(name, path string) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.configPath == "" {
		return errNoConfigPath
	}
	old := e.Config.Targets
	e.Config.Targets = append(append([]config.TargetEntry(nil), old...), config.TargetEntry{Name: name, Path: path})
	if err := config.Save(e.configPath, e.Config); err != nil {
		e.Config.Targets = old
		return err
	}
	return nil
}

// RemoveTarget drops the named Target and persists the Config. Removing a
// Target stops Skillmux from managing it; it does not delete already-installed
// Skills from disk.
func (e *Engine) RemoveTarget(name string) error {
	return e.mutate(func() { e.Config.Targets = withoutTarget(e.Config.Targets, name) },
		func(old config.Config) { e.Config.Targets = old.Targets })
}

// UpdateTarget replaces the Target named oldName in place (preserving its
// position) and persists the Config. Editing only the path keeps the same name,
// which config.Save accepts since the entry is replaced, not duplicated.
func (e *Engine) UpdateTarget(oldName, name, path string) error {
	return e.mutate(func() {
		e.Config.Targets = replaceTarget(e.Config.Targets, oldName, config.TargetEntry{Name: name, Path: path})
	}, func(old config.Config) { e.Config.Targets = old.Targets })
}

// AddSource appends a Source and persists the Config.
func (e *Engine) AddSource(s config.SourceEntry) error {
	return e.mutate(func() { e.Config.Sources = append(e.Config.Sources, s) },
		func(old config.Config) { e.Config.Sources = old.Sources })
}

// RemoveSource drops the named Source and persists the Config.
func (e *Engine) RemoveSource(name string) error {
	return e.mutate(func() { e.Config.Sources = withoutSource(e.Config.Sources, name) },
		func(old config.Config) { e.Config.Sources = old.Sources })
}

// ClearSourceCache removes the named Source's on-disk cache so the next Refresh
// re-downloads it. It reports whether the Source was cacheable: false (no error)
// for local Sources, which have no cache. Unlike the other ops it touches no
// Config, so nothing is persisted.
func (e *Engine) ClearSourceCache(name string) (bool, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	for _, src := range e.Config.DomainSources() {
		if src.Name == name {
			return e.Fetcher.ClearCache(src)
		}
	}
	return false, fmt.Errorf("source %q not found", name)
}

// UpdateSource replaces the Source named oldName in place and persists.
func (e *Engine) UpdateSource(oldName string, s config.SourceEntry) error {
	return e.mutate(func() { e.Config.Sources = replaceSource(e.Config.Sources, oldName, s) },
		func(old config.Config) { e.Config.Sources = old.Sources })
}

// ToggleSuggestion flips the classification of the edge from→to and persists the
// Config: a Dependency becomes a Suggestion (recorded as a `[[suggestion]]`
// from/to pair) and a Suggestion becomes a Dependency (the pair is dropped). It
// reports the new state (true == now a Suggestion). A router-wide bulk entry
// (To empty) is not split by this — callers should consult HasBulkSuggestion and
// steer the user to the hand-editable TOML instead. On a persist failure the
// in-memory Config is rolled back and nothing is written.
func (e *Engine) ToggleSuggestion(from, to string) (nowSuggestion bool, err error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.configPath == "" {
		return false, errNoConfigPath
	}
	old := append([]config.SuggestionEntry(nil), e.Config.Suggestions...)
	nowSuggestion = !e.Config.IsSuggestion(from, to)
	if nowSuggestion {
		e.Config.AddSuggestion(from, to)
	} else {
		e.Config.RemoveSuggestion(from, to)
	}
	if err := config.Save(e.configPath, e.Config); err != nil {
		e.Config.Suggestions = old
		return false, err
	}
	return nowSuggestion, nil
}

// mutate applies change, persists, and rolls back via restore on failure.
func (e *Engine) mutate(change func(), restore func(old config.Config)) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.configPath == "" {
		return errNoConfigPath
	}
	old := config.Config{
		Targets: append([]config.TargetEntry(nil), e.Config.Targets...),
		Sources: append([]config.SourceEntry(nil), e.Config.Sources...),
	}
	change()
	if err := config.Save(e.configPath, e.Config); err != nil {
		restore(old)
		return err
	}
	return nil
}

func withoutTarget(ts []config.TargetEntry, name string) []config.TargetEntry {
	out := make([]config.TargetEntry, 0, len(ts))
	for _, t := range ts {
		if t.Name != name {
			out = append(out, t)
		}
	}
	return out
}

func replaceTarget(ts []config.TargetEntry, oldName string, repl config.TargetEntry) []config.TargetEntry {
	out := append([]config.TargetEntry(nil), ts...)
	for i := range out {
		if out[i].Name == oldName {
			out[i] = repl
			break
		}
	}
	return out
}

func replaceSource(ss []config.SourceEntry, oldName string, repl config.SourceEntry) []config.SourceEntry {
	out := append([]config.SourceEntry(nil), ss...)
	for i := range out {
		if out[i].Name == oldName {
			out[i] = repl
			break
		}
	}
	return out
}

func withoutSource(ss []config.SourceEntry, name string) []config.SourceEntry {
	out := make([]config.SourceEntry, 0, len(ss))
	for _, s := range ss {
		if s.Name != name {
			out = append(out, s)
		}
	}
	return out
}
