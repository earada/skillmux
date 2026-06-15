package source

import (
	"os"
	"path/filepath"
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
