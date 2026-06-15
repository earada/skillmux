// Package manifest loads and saves the Skillmux-owned Manifest: the record of
// Installations and their version fingerprints. It is machine-managed (not
// hand-edited) and is the basis for detecting Update available. See CONTEXT.md.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/earada/skillmux/internal/domain"
)

// Manifest is the set of recorded Installations, keyed for lookup by the
// (Target, Skill) pair.
type Manifest struct {
	// Version lets the on-disk format evolve.
	Version int `json:"version"`
	// Installations is the flat list persisted to disk.
	Installations []domain.Installation `json:"installations"`
}

const currentVersion = 1

// Load reads the Manifest at path. A missing file yields an empty Manifest, not
// an error (nothing has been installed yet).
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Manifest{Version: currentVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &m, nil
}

// Save atomically writes the Manifest to path, creating parent directories as
// needed. It writes to a temp file then renames so a crash cannot leave a
// half-written Manifest.
func Save(path string, m *Manifest) error {
	m.Version = currentVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing manifest: %w", err)
	}
	return nil
}

// Find returns the Installation for a (target, skill) pair, if present.
func (m *Manifest) Find(targetName, skillName string) (domain.Installation, bool) {
	for _, in := range m.Installations {
		if in.TargetName == targetName && in.SkillName == skillName {
			return in, true
		}
	}
	return domain.Installation{}, false
}

// Put inserts or replaces the Installation for its (target, skill) pair.
func (m *Manifest) Put(in domain.Installation) {
	for i, existing := range m.Installations {
		if existing.TargetName == in.TargetName && existing.SkillName == in.SkillName {
			m.Installations[i] = in
			return
		}
	}
	m.Installations = append(m.Installations, in)
}

// Remove deletes the Installation for a (target, skill) pair, if present. It
// reports whether something was removed.
func (m *Manifest) Remove(targetName, skillName string) bool {
	for i, in := range m.Installations {
		if in.TargetName == targetName && in.SkillName == skillName {
			m.Installations = append(m.Installations[:i], m.Installations[i+1:]...)
			return true
		}
	}
	return false
}
