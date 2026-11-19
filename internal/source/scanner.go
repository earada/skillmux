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
	"regexp"
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
// root itself is a Skill). Scan fails only on a walk/IO error; a malformed
// Skill is skipped (see ScanWithWarnings for the reasons).
func Scan(root, sourceName string) ([]domain.Skill, error) {
	skills, _, err := ScanWithWarnings(root, sourceName)
	return skills, err
}

// ScanWithWarnings is Scan that also returns one warning per skipped Skill. A
// Skill directory whose SKILL.md is a symlink, has malformed frontmatter or an
// invalid name, or declares a name already seen earlier in the (lexical) walk
// is skipped and reported rather than failing the whole Source: one broken
// Skill in a third-party repo must not block every update from it
// (skillmux-xol). A skipped Skill directory is still not descended into.
func ScanWithWarnings(root, sourceName string) ([]domain.Skill, []string, error) {
	var skills []domain.Skill
	var warnings []string
	// seenBy maps a Skill name to the RelPath where it was first found, so a
	// second directory declaring the same name can be rejected. A name is a
	// Skill's identity within a Source; two directories sharing one would make
	// indistinguishable catalog rows and an install candidate that depends on
	// scan order. See skillmux-5r0.
	seenBy := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && domain.IsVCSMetadata(d.Name()) {
			return fs.SkipDir // VCS metadata is not Skill content (skillmux-ia0)
		}
		skillFile := filepath.Join(path, skillFileName)
		info, statErr := os.Lstat(skillFile)
		if statErr != nil {
			return nil // no SKILL.md here; keep descending
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// A symlinked SKILL.md is discoverable here — os.Stat would follow it
			// and read valid frontmatter — but fingerprint.Dir and apply.copyDir
			// copy only regular files, so they skip it. Cataloguing such a
			// directory would produce an installed Skill missing the very file
			// that defines it and a fingerprint that omits it. Reject it at the
			// source so scan, fingerprint, view, and apply all agree on what a
			// Skill is. See skillmux-iot.
			warnings = append(warnings, fmt.Sprintf("%s: SKILL.md is a symlink; a Skill's SKILL.md must be a regular file", skillFile))
			return fs.SkipDir
		}
		if info.IsDir() {
			return nil // a directory named SKILL.md does not define a Skill; keep descending
		}

		fm, err := parseFrontmatter(skillFile)
		if err != nil {
			var pathErr *fs.PathError
			if errors.As(err, &pathErr) {
				return fmt.Errorf("%s: %w", skillFile, err) // a real IO failure, not a malformed Skill
			}
			warnings = append(warnings, fmt.Sprintf("%s: %v", skillFile, err))
			return fs.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if firstRel, dup := seenBy[fm.Name]; dup {
			// The walk is lexical, so the first occurrence is deterministic; keep
			// it and skip this one.
			warnings = append(warnings, fmt.Sprintf("source %q declares duplicate skill %q at %s and %s; skipping %s",
				sourceName, fm.Name, filepath.ToSlash(firstRel), filepath.ToSlash(rel), filepath.ToSlash(rel)))
			return fs.SkipDir
		}
		seenBy[fm.Name] = rel
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
		return nil, nil, err
	}
	return skills, warnings, nil
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
		// Retry once with plain values quoted: upstream skills often write
		// `description: foo: bar`, which strict YAML rejects but agents accept.
		fm = frontmatter{}
		if lenient, ok := quotePlainValues(block); !ok || yaml.Unmarshal([]byte(lenient), &fm) != nil {
			return fm, fmt.Errorf("invalid frontmatter: %w", err)
		}
	}
	if strings.TrimSpace(fm.Name) == "" {
		return fm, errors.New("frontmatter missing required 'name'")
	}
	if err := validateSkillName(fm.Name); err != nil {
		return fm, err
	}
	return fm, nil
}

// topLevelKV matches an unindented `key: value` line.
var topLevelKV = regexp.MustCompile(`^([A-Za-z0-9_-]+):[ \t]+(.+)$`)

// quotePlainValues rewrites top-level `key: value` lines whose plain value
// contains ": " into single-quoted scalars, so a stray colon in a description
// does not break the whole block. Values that already start a YAML construct
// (quote, flow collection, block scalar, anchor, tag, ...) are left alone. It
// reports false when nothing needed rewriting, so callers keep the strict error.
func quotePlainValues(block string) (string, bool) {
	lines := strings.Split(block, "\n")
	changed := false
	for i, line := range lines {
		m := topLevelKV.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		val := strings.TrimRight(m[2], " \t\r")
		if !strings.Contains(val, ": ") || strings.ContainsAny(val[:1], `'"[{|>&*!%@`+"`") {
			continue
		}
		lines[i] = m[1] + ": '" + strings.ReplaceAll(val, "'", "''") + "'"
		changed = true
	}
	return strings.Join(lines, "\n"), changed
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
