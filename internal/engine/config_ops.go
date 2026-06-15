package engine

import (
	"errors"

	"github.com/earada/skillmux/internal/config"
)

// errNoConfigPath is returned when a mutation is attempted but the Engine was
// built without a Config path to persist to.
var errNoConfigPath = errors.New("no config path configured; cannot persist changes")

// AddTarget appends a Target and persists the Config. Validation (duplicate or
// empty name/path) is enforced by config.Save; on failure the in-memory Config
// is rolled back and nothing is written.
func (e *Engine) AddTarget(name, path string) error {
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

// mutate applies change, persists, and rolls back via restore on failure.
func (e *Engine) mutate(change func(), restore func(old config.Config)) error {
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

func withoutSource(ss []config.SourceEntry, name string) []config.SourceEntry {
	out := make([]config.SourceEntry, 0, len(ss))
	for _, s := range ss {
		if s.Name != name {
			out = append(out, s)
		}
	}
	return out
}
