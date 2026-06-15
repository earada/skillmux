// Package source discovers Skills within a Source. A Source (a local folder or
// an extracted GitHub tarball) is scanned recursively: every directory holding
// a SKILL.md is one Skill, and Skillmux does not descend into a Skill once
// found. See CONTEXT.md and the design Q14.
package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/earada/skillmux/internal/domain"
)

const skillFileName = "SKILL.md"

// utf8BOM is the byte-order mark some editors prepend to UTF-8 files.
const utf8BOM = "\uFEFF"

// Scan walks root and returns the Skills found in it, attributing each to
// sourceName. Each Skill's RelPath is its directory relative to root ("." when
// root itself is a Skill). Scan fails on the first malformed SKILL.md so the
// user gets a clear pointer to fix it.
func Scan(root, sourceName string) ([]domain.Skill, error) {
	var skills []domain.Skill
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		skillFile := filepath.Join(path, skillFileName)
		info, statErr := os.Stat(skillFile)
		if statErr != nil || info.IsDir() {
			return nil // no SKILL.md here; keep descending
		}

		fm, err := parseFrontmatter(skillFile)
		if err != nil {
			return fmt.Errorf("%s: %w", skillFile, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		skills = append(skills, domain.Skill{
			Name:        fm.Name,
			Description: fm.Description,
			SourceName:  sourceName,
			RelPath:     rel,
		})
		return fs.SkipDir // a Skill is atomic; do not descend into it
	})
	if err != nil {
		return nil, err
	}
	return skills, nil
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// parseFrontmatter extracts the leading YAML frontmatter block (delimited by
// `---` fences) from a SKILL.md and requires a non-empty name, which is the
// Skill's identity.
func parseFrontmatter(path string) (frontmatter, error) {
	var fm frontmatter
	data, err := os.ReadFile(path)
	if err != nil {
		return fm, err
	}
	content := strings.TrimPrefix(string(data), utf8BOM)
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, errors.New("missing YAML frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return fm, errors.New("unterminated YAML frontmatter")
	}
	block := strings.Join(lines[1:end], "\n")
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return fm, fmt.Errorf("invalid frontmatter: %w", err)
	}
	if strings.TrimSpace(fm.Name) == "" {
		return fm, errors.New("frontmatter missing required 'name'")
	}
	return fm, nil
}
