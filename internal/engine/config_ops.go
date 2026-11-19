package engine

import (
	"errors"
	"fmt"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/manifest"
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
// Skills from disk. Once the Config is persisted, the Target's Installations
// are dropped from the Manifest (and the Manifest saved) so they are not
// planned as uninstalls against a Target that no longer exists (skillmux-fq2);
// the files themselves stay where they are.
func (e *Engine) RemoveTarget(name string) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.configPath == "" {
		return errNoConfigPath
	}
	old := e.Config.Targets
	e.Config.Targets = withoutTarget(old, name)
	if err := config.Save(e.configPath, e.Config); err != nil {
		e.Config.Targets = old
		return err
	}
	return e.forgetTargetInstallations(name)
}

// forgetTargetInstallations drops every Installation recorded for target from
// the Manifest and persists it. Files on disk are not touched. The caller must
// hold opMu.
func (e *Engine) forgetTargetInstallations(target string) error {
	kept := e.Manifest.Installations[:0:0]
	for _, in := range e.Manifest.Installations {
		if in.TargetName != target {
			kept = append(kept, in)
		}
	}
	if len(kept) == len(e.Manifest.Installations) {
		return nil
	}
	e.Manifest.Installations = kept
	return manifest.Save(e.manifestPath, e.Manifest)
}

// UpdateTarget replaces the Target named oldName in place (preserving its
// position) and persists the Config. Editing only the path keeps the same name,
// which config.Save accepts since the entry is replaced, not duplicated.
// Renaming the Target also re-keys its Installations in the Manifest (which are
// keyed by TargetName) and persists it, so the renamed Target keeps its
// installed Skills instead of orphaning them under the old name.
func (e *Engine) UpdateTarget(oldName, name, path string) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.configPath == "" {
		return errNoConfigPath
	}
	old := e.Config.Targets
	e.Config.Targets = replaceTarget(old, oldName, config.TargetEntry{Name: name, Path: path})
	if err := config.Save(e.configPath, e.Config); err != nil {
		e.Config.Targets = old
		return err
	}
	if oldName == name {
		return nil
	}
	return e.renameInstallations(oldName, name)
}

// renameInstallations rewrites TargetName from oldName to name on every
// Installation in the Manifest and persists it. Recorded Paths are left as-is
// so a simultaneous path edit is still detected as a target move. The caller
// must hold opMu.
func (e *Engine) renameInstallations(oldName, name string) error {
	changed := false
	for i := range e.Manifest.Installations {
		if e.Manifest.Installations[i].TargetName == oldName {
			e.Manifest.Installations[i].TargetName = name
			changed = true
		}
	}
	if !changed || e.manifestPath == "" {
		return nil
	}
	return manifest.Save(e.manifestPath, e.Manifest)
}

// AddSource appends a Source and persists the Config.
func (e *Engine) AddSource(s config.SourceEntry) error {
	return e.mutate(func() { e.Config.Sources = append(e.Config.Sources, s) },
		func(old config.Config) { e.Config.Sources = old.Sources })
}

// RemoveSource drops the named Source and persists the Config. Once the save
// succeeds, the Source's cached clone (if any) is deleted, best-effort: a
// cleanup failure does not fail the removal.
func (e *Engine) RemoveSource(name string) error {
	return e.mutateSources(func(ss []config.SourceEntry) []config.SourceEntry { return withoutSource(ss, name) },
		name, func(prev domain.Source) { _, _ = e.Fetcher.ClearCache(prev) })
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

// UpdateSource replaces the Source named oldName in place and persists. When
// the name changes, the cached clone (keyed by name) is moved to the new key
// once the save succeeds, best-effort, so it is reused rather than orphaned.
// Location/branch/subpath edits leave the cache alone (Fetch re-syncs origin).
func (e *Engine) UpdateSource(oldName string, s config.SourceEntry) error {
	return e.mutateSources(func(ss []config.SourceEntry) []config.SourceEntry { return replaceSource(ss, oldName, s) },
		oldName, func(prev domain.Source) {
			if s.Name == oldName {
				return
			}
			next := prev
			next.Name = s.Name
			_ = e.Fetcher.MoveCache(prev, next)
		})
}

// mutateSources is mutate for Source edits, plus a post-save cache step run in
// the same opMu critical section (so no Refresh is using the clone). afterSave
// receives the Source named name as it was before the change; it is skipped if
// the save fails or no such Source existed.
func (e *Engine) mutateSources(change func([]config.SourceEntry) []config.SourceEntry, name string, afterSave func(prev domain.Source)) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.configPath == "" {
		return errNoConfigPath
	}
	var prev domain.Source
	found := false
	for _, src := range e.Config.DomainSources() {
		if src.Name == name {
			prev, found = src, true
			break
		}
	}
	old := e.Config.Sources
	e.Config.Sources = change(append([]config.SourceEntry(nil), old...))
	if err := config.Save(e.configPath, e.Config); err != nil {
		e.Config.Sources = old
		return err
	}
	if found {
		afterSave(prev)
	}
	return nil
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

// TogglePin freezes or thaws one matrix row — the Skill named skill as supplied
// by source — and persists the Config as a `[[pin]]` entry. It reports the new
// state (true == now pinned). A wider entry (Skill-wide or Source-wide) is not
// narrowed by this: callers should consult HasWidePin and steer the user to the
// hand-editable TOML instead. On a persist failure the in-memory Config is rolled
// back and nothing is written.
func (e *Engine) TogglePin(skill, source string) (nowPinned bool, err error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.configPath == "" {
		return false, errNoConfigPath
	}
	old := append([]config.PinEntry(nil), e.Config.Pins...)
	nowPinned = !e.Config.IsPinned(skill, source)
	if nowPinned {
		e.Config.AddPin(skill, source)
	} else {
		e.Config.RemovePin(skill, source)
	}
	if err := config.Save(e.configPath, e.Config); err != nil {
		e.Config.Pins = old
		return false, err
	}
	return nowPinned, nil
}

// IsPinned reports whether the (skill, source) row is frozen by a Pin.
func (e *Engine) IsPinned(skill, source string) bool { return e.Config.IsPinned(skill, source) }

// HasWidePin reports whether the row is frozen only by a Pin broader than itself,
// which TogglePin cannot lift.
func (e *Engine) HasWidePin(skill, source string) bool { return e.Config.HasWidePin(skill, source) }

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
