package source

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeSkill creates dir/SKILL.md with the given frontmatter body.
func writeSkill(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const deployMD = `---
name: deploy
description: Deploy the app
---
# Deploy
do things
`

func TestScanFindsSkillsRecursively(t *testing.T) {
	root := t.TempDir()
	// Flat skill, nested skill, and a non-skill directory in between.
	writeSkill(t, filepath.Join(root, "deploy"), deployMD)
	writeSkill(t, filepath.Join(root, "category", "lint"), `---
name: lint
description: Lint code
---
body`)
	if err := os.MkdirAll(filepath.Join(root, "category", "notaskill"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Scan(root, "mysrc")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(got), got)
	}
	byName := map[string]string{} // name -> relpath
	for _, s := range got {
		byName[s.Name] = s.RelPath
		if s.SourceName != "mysrc" {
			t.Errorf("skill %q: SourceName = %q, want mysrc", s.Name, s.SourceName)
		}
	}
	if byName["deploy"] != "deploy" {
		t.Errorf("deploy relpath = %q, want deploy", byName["deploy"])
	}
	if byName["lint"] != filepath.Join("category", "lint") {
		t.Errorf("lint relpath = %q, want category/lint", byName["lint"])
	}
}

func TestScanDoesNotDescendIntoASkill(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "outer"), deployMD)
	// A nested SKILL.md inside a skill must be ignored.
	writeSkill(t, filepath.Join(root, "outer", "inner"), `---
name: inner
description: should be ignored
---`)

	got, err := Scan(root, "s")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0].Name != "deploy" {
		t.Fatalf("expected only the outer skill, got %+v", got)
	}
}

func TestScanParsesNameAndDescription(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "deploy"), deployMD)
	got, err := Scan(root, "s")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got[0].Description != "Deploy the app" {
		t.Errorf("description = %q, want %q", got[0].Description, "Deploy the app")
	}
}

func TestScanRejectsSkillWithoutName(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "bad"), `---
description: no name here
---`)
	if _, err := Scan(root, "s"); err == nil {
		t.Fatal("expected error for SKILL.md without a name")
	}
}

func TestScanRejectsMissingFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "bad"), "# Just a heading, no frontmatter")
	if _, err := Scan(root, "s"); err == nil {
		t.Fatal("expected error for SKILL.md without frontmatter")
	}
}

func TestScanDerivesGroupFromFolders(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "deploy"), deployMD) // root-level: no group
	writeSkill(t, filepath.Join(root, "typescript", "strict-mode"), `---
name: strict-mode
---`) // one level: group "typescript"
	writeSkill(t, filepath.Join(root, "frontend", "react", "use-effect"), `---
name: use-effect
---`) // nested: group "frontend/react"

	got, err := Scan(root, "s")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	groups := map[string]string{} // name -> group
	for _, s := range got {
		groups[s.Name] = s.Group
	}
	if groups["strict-mode"] != "typescript" {
		t.Errorf("strict-mode group = %q, want typescript", groups["strict-mode"])
	}
	if groups["use-effect"] != "frontend/react" {
		t.Errorf("use-effect group = %q, want frontend/react", groups["use-effect"])
	}
	// A top-level skill (single path segment) has no group.
	if groups["deploy"] != "" {
		t.Errorf("top-level skill group = %q, want empty", groups["deploy"])
	}
}

func TestScanParsesDeprecated(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "boolform"), `---
name: boolform
deprecated: true
---`)
	writeSkill(t, filepath.Join(root, "reasonform"), `---
name: reasonform
deprecated: use new-skill instead
---`)
	writeSkill(t, filepath.Join(root, "live"), `---
name: live
---`)
	writeSkill(t, filepath.Join(root, "explicitfalse"), `---
name: explicitfalse
deprecated: false
---`)

	got, err := Scan(root, "s")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	type dep struct {
		deprecated bool
		reason     string
	}
	byName := map[string]dep{}
	for _, s := range got {
		byName[s.Name] = dep{s.Deprecated, s.DeprecationReason}
	}
	if d := byName["boolform"]; !d.deprecated || d.reason != "" {
		t.Errorf("boolform = %+v, want deprecated with no reason", d)
	}
	if d := byName["reasonform"]; !d.deprecated || d.reason != "use new-skill instead" {
		t.Errorf("reasonform = %+v, want deprecated with reason", d)
	}
	if d := byName["live"]; d.deprecated {
		t.Errorf("live = %+v, want not deprecated", d)
	}
	if d := byName["explicitfalse"]; d.deprecated {
		t.Errorf("explicitfalse = %+v, want not deprecated", d)
	}
}

func TestScanRejectsNamesThatEscapeTarget(t *testing.T) {
	// A Skill's name is later joined onto a Target directory, so a name with
	// separators or dot components could resolve outside it. The scanner must
	// reject such names before they enter the catalog. See skillmux-aps.
	cases := map[string]string{
		"parent-traversal":  "../victim",
		"nested-traversal":  "foo/../../victim",
		"backslash":         `..\victim`,
		"forward-slash":     "sub/dir",
		"dot":               ".",
		"dotdot":            "..",
		"absolute":          "/etc/passwd",
		"leading-space":     " deploy",
		"trailing-space":    "deploy ",
		"control-character": "dep\x00loy",
		"newline":           "dep\nloy",
	}
	for label, name := range cases {
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			writeSkill(t, filepath.Join(root, "skill"), "---\nname: "+strconv.Quote(name)+"\n---\nbody")
			if _, err := Scan(root, "s"); err == nil {
				t.Fatalf("expected error for skill name %q, got none", name)
			}
		})
	}
}

func TestScanAcceptsCanonicalNames(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skill"), "---\nname: my-skill.v2\n---\nbody")
	got, err := Scan(root, "s")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0].Name != "my-skill.v2" {
		t.Fatalf("expected single skill named my-skill.v2, got %+v", got)
	}
}

func TestScanRejectsDuplicateSkillNames(t *testing.T) {
	// Two sibling directories declaring the same name are an ambiguous identity
	// within one Source: the catalog would show indistinguishable rows and the
	// install candidate would depend on scan order. See skillmux-5r0.
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a"), `---
name: dup
description: first
---
body`)
	writeSkill(t, filepath.Join(root, "b"), `---
name: dup
description: second
---
body`)

	_, err := Scan(root, "mysrc")
	if err == nil {
		t.Fatal("expected error for duplicate skill names, got none")
	}
	msg := err.Error()
	for _, want := range []string{"dup", "mysrc", "a", "b"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}

func TestScanRejectsDuplicateSkillNamesInSiblingGroups(t *testing.T) {
	// The same name under two different group directories is still one Source
	// declaring one identity twice, and must be rejected with both relpaths.
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "team-a", "deploy"), `---
name: deploy
description: team a
---
body`)
	writeSkill(t, filepath.Join(root, "team-b", "deploy"), `---
name: deploy
description: team b
---
body`)

	_, err := Scan(root, "s")
	if err == nil {
		t.Fatal("expected error for duplicate names across groups, got none")
	}
	msg := err.Error()
	if !strings.Contains(msg, "team-a/deploy") || !strings.Contains(msg, "team-b/deploy") {
		t.Errorf("error %q should include both relpaths team-a/deploy and team-b/deploy", msg)
	}
}

func TestScanDuplicateErrorIsStable(t *testing.T) {
	// Both relpaths appear in a fixed (sorted) order regardless of which
	// directory the walk visits first, so callers get stable, testable output.
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "zeta"), `---
name: dup
---
body`)
	writeSkill(t, filepath.Join(root, "alpha"), `---
name: dup
---
body`)

	_, err := Scan(root, "s")
	if err == nil {
		t.Fatal("expected error, got none")
	}
	// "alpha" sorts before "zeta" and must be named first in the message.
	msg := err.Error()
	ai, zi := strings.Index(msg, "alpha"), strings.Index(msg, "zeta")
	if ai == -1 || zi == -1 || ai > zi {
		t.Errorf("error %q should mention alpha before zeta", msg)
	}
}

func TestScanRejectsSymlinkedSkillFile(t *testing.T) {
	// A directory whose SKILL.md is a symlink is discoverable (os.Stat follows
	// the link to valid frontmatter) but fingerprint.Dir and apply.copyDir copy
	// only regular files, so the installed folder would lack the defining
	// SKILL.md. The scanner must reject it before it reaches the catalog. See
	// skillmux-iot.
	root := t.TempDir()
	// A valid SKILL.md living outside the skill directory.
	realDir := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(realDir, "real.md")
	if err := os.WriteFile(realFile, []byte(deployMD), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(root, "linked")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realFile, filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	_, err := Scan(root, "s")
	if err == nil {
		t.Fatal("expected error for symlinked SKILL.md, got none")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error %q should explain the SKILL.md is a symlink", err)
	}
}

func TestScanRootIsItselfASkill(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, deployMD)
	got, err := Scan(root, "s")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0].RelPath != "." {
		t.Fatalf("expected single root skill with relpath '.', got %+v", got)
	}
}
