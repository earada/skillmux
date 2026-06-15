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

// Config is the on-disk shape of the user's declared Targets and Sources.
type Config struct {
	Targets []TargetEntry `toml:"target"`
	Sources []SourceEntry `toml:"source"`
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

// Save writes the Config to path, creating parent directories as needed.
func Save(path string, c *Config) error {
	if err := c.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
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
	return nil
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
