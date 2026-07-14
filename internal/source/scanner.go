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
	"unicode"

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
			Name:              fm.Name,
			Description:       fm.Description,
			SourceName:        sourceName,
			RelPath:           rel,
			Group:             groupOf(rel),
			Deprecated:        fm.Deprecated.deprecated,
			DeprecationReason: fm.Deprecated.reason,
		})
		return fs.SkipDir // a Skill is atomic; do not descend into it
	})
	if err != nil {
		return nil, err
	}
	return skills, nil
}

type frontmatter struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Deprecated  deprecation `yaml:"deprecated"`
}

// deprecation captures the optional `deprecated` frontmatter field, which an
// author may write either as a bool (`deprecated: true`) or as a string giving
// a migration note (`deprecated: "use new-skill instead"`). Both forms mark the
// Skill deprecated; the string form also carries a reason.
type deprecation struct {
	deprecated bool
	reason     string
}

func (d *deprecation) UnmarshalYAML(value *yaml.Node) error {
	var b bool
	if err := value.Decode(&b); err == nil {
		d.deprecated = b
		return nil
	}
	var s string
	if err := value.Decode(&s); err == nil {
		d.reason = strings.TrimSpace(s)
		d.deprecated = d.reason != ""
		return nil
	}
	return fmt.Errorf("invalid 'deprecated' value: want a bool or a string, got %q", value.Value)
}

// groupOf derives a Skill's Group from its path relative to the Source root: the
// parent directories, slash-joined for stable cross-platform display. A Skill at
// the root ("." or a single segment) has no group.
func groupOf(rel string) string {
	dir := filepath.Dir(rel)
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return filepath.ToSlash(dir)
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
	if err := validateSkillName(fm.Name); err != nil {
		return fm, err
	}
	return fm, nil
}

// validateSkillName requires a Skill's name to be a canonical single path
// component before it enters the catalog. The name is later joined onto a
// Target directory (see apply.install/uninstall), so a name carrying separators
// or dot components — e.g. "../victim" — could resolve outside the Target and
// let an install create, or a later uninstall recursively delete, arbitrary
// sibling paths. Rejecting such names here is the primary defence; apply keeps a
// containment backstop for anything that still slips through. See skillmux-aps.
func validateSkillName(name string) error {
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("skill name %q has surrounding whitespace", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("skill name %q is a dot path component", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("skill name %q contains a path separator", name)
	}
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return fmt.Errorf("skill name %q is an absolute path", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("skill name %q contains a control character", name)
		}
	}
	// Backstop: after all the checks above, the name must still be exactly its
	// own last path element — a single, canonical component.
	if filepath.Base(name) != name {
		return fmt.Errorf("skill name %q is not a single path component", name)
	}
	return nil
}
