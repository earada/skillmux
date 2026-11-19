package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scanSkipsOnly scans root, requires no error, no Skills and exactly one
// warning, and returns that warning. Used by the tests asserting a malformed
// Skill is skipped rather than failing the Source (skillmux-xol).
func scanSkipsOnly(t *testing.T, root, sourceName string) string {
	t.Helper()
	skills, warnings, err := ScanWithWarnings(root, sourceName)
	if err != nil {
		t.Fatalf("a malformed Skill must not fail the scan: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected the malformed Skill to be skipped, got %+v", skills)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %q", warnings)
	}
	return warnings[0]
}

func TestScanSkipsBadSkillAndKeepsGoodOnes(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "a-broken"), "# no frontmatter")
	writeSkill(t, filepath.Join(root, "b-noname"), "---\ndescription: x\n---\n")
	writeSkill(t, filepath.Join(root, "deploy"), deployMD)
	// A Skill nested inside a skipped Skill directory is not discovered: a
	// directory holding a SKILL.md is still atomic even when it is skipped.
	writeSkill(t, filepath.Join(root, "a-broken", "inner"), "---\nname: inner\n---\n")

	skills, warnings, err := ScanWithWarnings(root, "s")
	if err != nil {
		t.Fatalf("ScanWithWarnings: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "deploy" {
		t.Fatalf("expected only 'deploy', got %+v", skills)
	}
	if len(warnings) != 2 ||
		!strings.Contains(warnings[0], filepath.Join("a-broken", "SKILL.md")) ||
		!strings.Contains(warnings[1], filepath.Join("b-noname", "SKILL.md")) {
		t.Fatalf("expected warnings for a-broken and b-noname in walk order, got %q", warnings)
	}

	// Scan keeps its signature and drops the warnings.
	plain, err := Scan(root, "s")
	if err != nil || len(plain) != 1 {
		t.Fatalf("Scan: skills=%+v err=%v", plain, err)
	}
}

func TestScanDuplicateKeepsFirstInWalkOrder(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "zeta"), "---\nname: dup\ndescription: z\n---\n")
	writeSkill(t, filepath.Join(root, "alpha"), "---\nname: dup\ndescription: a\n---\n")

	skills, warnings, err := ScanWithWarnings(root, "s")
	if err != nil {
		t.Fatalf("ScanWithWarnings: %v", err)
	}
	if len(skills) != 1 || skills[0].RelPath != "alpha" {
		t.Fatalf("expected the lexically first 'alpha' kept, got %+v", skills)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "alpha") || !strings.Contains(warnings[0], "zeta") {
		t.Fatalf("expected one duplicate warning naming both paths, got %q", warnings)
	}
}

func TestScanUnreadableSkillFileIsStillFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "locked")
	writeSkill(t, dir, deployMD)
	if err := os.Chmod(filepath.Join(dir, "SKILL.md"), 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ScanWithWarnings(root, "s"); err == nil {
		t.Fatal("an IO failure reading SKILL.md must still fail the scan")
	}
}
