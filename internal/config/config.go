// Package config loads and saves the user-owned Config: the declared Targets
// and Sources. The TOML file is the readable source of truth; the TUI may also
// edit it. See CONTEXT.md.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/earada/skillmux/internal/domain"
)

// Config is the on-disk shape of the user's declared Targets and Sources, plus
// any manual Suggestion reclassifications.
type Config struct {
	Targets     []TargetEntry     `toml:"target"`
	Sources     []SourceEntry     `toml:"source"`
	Suggestions []SuggestionEntry `toml:"suggestion,omitempty"`
}

// SuggestionEntry downgrades an inferred Dependency edge to a Suggestion: the
// edge from Skill From to Skill To is advisory, not a hard requirement, so
// Skillmux must not warn on it. To may be empty, which marks every outgoing edge
// of From a Suggestion — the bulk form for a router Skill like `ask-matt`.
// Skills are named by identity (the SKILL.md `name`), not qualified by Source.
type SuggestionEntry struct {
	From string `toml:"from"`
	To   string `toml:"to,omitempty"`
}

// TargetEntry is one configured Target.
type TargetEntry struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

// SourceEntry is one configured Source. Kind is inferred from Location when
// omitted (a URL is github, anything else is local).
type SourceEntry struct {
	Name     string `toml:"name"`
	Location string `toml:"location"`
	Kind     string `toml:"kind,omitempty"`
	Branch   string `toml:"branch,omitempty"`
	Subpath  string `toml:"subpath,omitempty"`
}

// Load reads and validates the Config at path. A missing file yields an empty
// Config (manual setup starts from scratch in v1), not an error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save atomically writes the Config to path, creating parent directories as
// needed. It encodes to a same-directory temp file, flushes it to disk, then
// renames over path so a crash, short write, or interruption cannot destroy the
// user-owned source of truth: the previous Config either survives intact or is
// replaced by a fully written one. Existing file permissions are preserved.
func Save(path string, c *Config) error {
	if err := c.validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	// Preserve the existing file's permissions; default to 0644 for a new file.
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	// Write to a temp file in the same directory so the final rename is atomic
	// (a cross-directory rename is not).
	tmp, err := os.CreateTemp(dir, ".config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("creating temp config: %w", err)
	}
	tmpPath := tmp.Name()
	// Clean up the temp file on any failure before the rename succeeds.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flushing config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing config: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("setting config permissions: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
}

func (c *Config) validate() error {
	seenT := map[string]bool{}
	for _, t := range c.Targets {
		if t.Name == "" || t.Path == "" {
			return fmt.Errorf("target %q: name and path are required", t.Name)
		}
		if seenT[t.Name] {
			return fmt.Errorf("duplicate target name %q", t.Name)
		}
		seenT[t.Name] = true
	}
	seenS := map[string]bool{}
	for _, s := range c.Sources {
		if s.Name == "" || s.Location == "" {
			return fmt.Errorf("source %q: name and location are required", s.Name)
		}
		if seenS[s.Name] {
			return fmt.Errorf("duplicate source name %q", s.Name)
		}
		seenS[s.Name] = true
	}
	for _, sg := range c.Suggestions {
		if sg.From == "" {
			return errors.New("suggestion: from is required")
		}
	}
	return nil
}

// IsSuggestion reports whether the edge from→to has been manually reclassified
// as a Suggestion: matched by an exact From/To pair, or by a bulk entry (To
// empty) that downgrades every outgoing edge of From.
func (c *Config) IsSuggestion(from, to string) bool {
	for _, sg := range c.Suggestions {
		if sg.From != from {
			continue
		}
		if sg.To == "" || sg.To == to {
			return true
		}
	}
	return false
}

// AddSuggestion records the edge from→to as a Suggestion. It is a no-op when the
// edge is already a Suggestion (including via a bulk From entry).
func (c *Config) AddSuggestion(from, to string) {
	if c.IsSuggestion(from, to) {
		return
	}
	c.Suggestions = append(c.Suggestions, SuggestionEntry{From: from, To: to})
}

// RemoveSuggestion drops the exact from→to pair, restoring it to a Dependency.
// It does not split a bulk From entry (To empty): a router-wide downgrade is
// removed only by deleting that entry, so the caller should check
// HasBulkSuggestion before offering a per-edge toggle.
func (c *Config) RemoveSuggestion(from, to string) {
	kept := c.Suggestions[:0]
	for _, sg := range c.Suggestions {
		if sg.From == from && sg.To == to {
			continue
		}
		kept = append(kept, sg)
	}
	c.Suggestions = kept
}

// HasBulkSuggestion reports whether From's outgoing edges are downgraded
// wholesale by a bulk entry (To empty), which a per-edge toggle cannot undo.
func (c *Config) HasBulkSuggestion(from string) bool {
	for _, sg := range c.Suggestions {
		if sg.From == from && sg.To == "" {
			return true
		}
	}
	return false
}

// DomainTargets returns the configured Targets as domain values.
func (c *Config) DomainTargets() []domain.Target {
	out := make([]domain.Target, 0, len(c.Targets))
	for _, t := range c.Targets {
		out = append(out, domain.Target{Name: t.Name, Path: t.Path})
	}
	return out
}

// DomainSources returns the configured Sources as domain values, inferring Kind
// from the Location when it is not set explicitly.
func (c *Config) DomainSources() []domain.Source {
	out := make([]domain.Source, 0, len(c.Sources))
	for _, s := range c.Sources {
		out = append(out, domain.Source{
			Name:     s.Name,
			Kind:     inferKind(s.Kind, s.Location),
			Location: s.Location,
			Branch:   s.Branch,
			Subpath:  s.Subpath,
		})
	}
	return out
}

func inferKind(explicit, location string) domain.SourceKind {
	switch explicit {
	case string(domain.SourceGitHub):
		return domain.SourceGitHub
	case string(domain.SourceLocal):
		return domain.SourceLocal
	}
	if strings.HasPrefix(location, "http://") ||
		strings.HasPrefix(location, "https://") ||
		strings.HasPrefix(location, "git@") {
		return domain.SourceGitHub
	}
	return domain.SourceLocal
}
