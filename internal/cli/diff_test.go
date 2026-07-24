package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffNothingInstalled(t *testing.T) {
	e, _, _ := newEnv(t)
	code, out, _ := run(e, "", "diff")
	if code != exitOK || !strings.Contains(out, "All installations match their source.") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestDiffUpToDateIsQuiet(t *testing.T) {
	e, _, _ := newEnv(t)
	install(t, e)
	code, out, _ := run(e, "", "diff")
	if code != exitOK {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if strings.Contains(out, "@@") {
		t.Errorf("an up-to-date installation should print no hunks:\n%s", out)
	}
}

func TestDiffShowsUpstreamChange(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2") // upstream drift
	if err := os.WriteFile(filepath.Join(skillDir, "run.sh"), []byte("echo go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, _ := run(e, "", "diff")
	if code != exitPending {
		t.Fatalf("differences should exit %d, got %d\n%s", exitPending, code, out)
	}
	for _, want := range []string{
		"deploy (local) → cc",
		"--- ",
		"+++ ",
		"2 file(s) changed: 1 added, 0 removed, 1 modified",
		"~ SKILL.md  modified",
		"@@ -",
		"-v1",
		"+v2",
		"+ run.sh  added",
		"+echo go",
		"1 installation(s) differ from their source.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff output missing %q:\n%s", want, out)
		}
	}
}

func TestDiffFlagsLocallyModifiedCopy(t *testing.T) {
	e, targetPath, _ := newEnv(t)
	install(t, e)
	modifyInstalled(t, targetPath)

	code, out, _ := run(e, "", "diff")
	if code != exitPending {
		t.Fatalf("a hand-edited copy differs from its source; code=%d\n%s", code, out)
	}
	if !strings.Contains(out, "! modified locally") {
		t.Errorf("expected the local-drift caveat:\n%s", out)
	}
	// The diff runs installed → upstream, so the hand edit reads as a deletion
	// and the source's content as the addition.
	if !strings.Contains(out, "-my local tweaks") {
		t.Errorf("expected the local edit on the old side:\n%s", out)
	}
}

// An untracked folder is not an Installation, so the default walk never reaches
// it — but naming the pair must still answer, flagged as untracked.
func TestDiffFlagsUntrackedFolder(t *testing.T) {
	e, targetPath, _ := newEnv(t)
	dir := filepath.Join(targetPath, "deploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The default walk sees no Installation, so it stays quiet.
	code, out, _ := run(e, "", "diff")
	if code != exitOK || strings.Contains(out, "@@") {
		t.Fatalf("untracked folders are not installations; code=%d out=%q", code, out)
	}
	// Asked for by name, it is compared and flagged.
	code, out, _ = run(e, "", "diff", "deploy", "cc")
	if code != exitPending {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "! untracked") || !strings.Contains(out, "-mine") {
		t.Errorf("expected an untracked comparison:\n%s", out)
	}
}

func TestDiffNarrowsToSkillAndTarget(t *testing.T) {
	e, _, skillDir := newEnv(t)
	writeSkillNamed(t, filepath.Join(filepath.Dir(skillDir), "other"), "other")
	install(t, e)
	writeSkill(t, skillDir, "v2")

	// An explicit pair that differs.
	code, out, _ := run(e, "", "diff", "deploy", "cc")
	if code != exitPending || !strings.Contains(out, "@@") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	// An explicit pair that does not: say so rather than exiting silently.
	run(e, "", "apply", "--yes")
	code, out, _ = run(e, "", "diff", "deploy")
	if code != exitOK || !strings.Contains(out, "no differences") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestDiffUnknownFilterErrors(t *testing.T) {
	e, _, _ := newEnv(t)
	install(t, e)
	code, _, errOut := run(e, "", "diff", "nope")
	if code != exitError || !strings.Contains(errOut, `no installation matches skill "nope"`) {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(e, "", "diff", "deploy", "nowhere")
	if code != exitError || !strings.Contains(errOut, "deploy → nowhere") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestDiffTooManyArgs(t *testing.T) {
	e, _, _ := newEnv(t)
	code, _, errOut := run(e, "", "diff", "a", "b", "c")
	if code != exitError || !strings.Contains(errOut, "at most") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestDiffFailingSourceExitsError(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	// The whole source vanishes: the cached snapshot stands in for upstream, so
	// the diff cannot be trusted and must not report a clean 0.
	if err := os.RemoveAll(filepath.Dir(skillDir)); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(e, "", "diff")
	if code != exitError || !strings.Contains(errOut, "source local") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

// File contents are Source-controlled and land straight on a terminal, so the
// printed diff must be inert (same invariant the TUI upholds).
func TestDiffSanitizesFileContent(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2\x1b]52;c;cGF5bG9hZA==\x07\rspoofed")

	code, out, _ := run(e, "", "diff")
	if code != exitPending {
		t.Fatalf("code=%d out=%q", code, out)
	}
	for _, bad := range []rune{0x1b, 0x07, '\r'} {
		if strings.ContainsRune(out, bad) {
			t.Fatalf("diff output leaked %#x: %q", bad, out)
		}
	}
	if !strings.Contains(out, "spoofed") {
		t.Errorf("visible text should survive sanitization:\n%s", out)
	}
}

func TestDiffHelpMentionsSubcommand(t *testing.T) {
	e, _, _ := newEnv(t)
	_, out, _ := run(e, "", "help")
	if !strings.Contains(out, "skillmux diff") {
		t.Errorf("help should document diff:\n%s", out)
	}
}
