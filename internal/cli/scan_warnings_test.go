package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addBrokenSkill drops a SKILL.md with no frontmatter next to "deploy" in the
// local Source, so the scan skips it with a warning (skillmux-xol).
func addBrokenSkill(t *testing.T, skillDir string) {
	t.Helper()
	bad := filepath.Join(filepath.Dir(skillDir), "broken")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanWarningPrintedWithoutChangingExitCode(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	addBrokenSkill(t, skillDir)

	for _, cmd := range []string{"status", "check"} {
		code, _, stderr := run(e, "", cmd)
		if code != exitOK {
			t.Errorf("%s: a skipped skill must not change the exit code, got %d\n%s", cmd, code, stderr)
		}
		if !strings.Contains(stderr, "skillmux: source local: warning: ") ||
			!strings.Contains(stderr, filepath.Join("broken", "SKILL.md")) {
			t.Errorf("%s: missing warning on stderr:\n%s", cmd, stderr)
		}
	}
}

func TestApplyNotRefusedByScanWarning(t *testing.T) {
	e, targetPath, skillDir := newEnv(t)
	install(t, e)
	addBrokenSkill(t, skillDir)
	writeSkill(t, skillDir, "v2") // upstream drift

	code, out, stderr := run(e, "", "apply", "--yes")
	if code != exitOK || strings.Contains(stderr, "refusing") {
		t.Fatalf("apply must not refuse over a skipped skill: code=%d\nstdout=%s\nstderr=%s", code, out, stderr)
	}
	if !strings.Contains(stderr, "warning:") {
		t.Errorf("expected the warning on stderr:\n%s", stderr)
	}
	got, err := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if err != nil || !strings.Contains(string(got), "v2") {
		t.Errorf("good skill not updated: err=%v content=%q", err, got)
	}
}
