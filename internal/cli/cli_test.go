package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// newEnv builds an Engine over one local Source holding the "deploy" skill and
// one Target "cc", mirroring the engine tests' fixture.
func newEnv(t *testing.T) (e *engine.Engine, targetPath, skillDir string) {
	t.Helper()
	srcRoot := t.TempDir()
	skillDir = filepath.Join(srcRoot, "deploy")
	writeSkill(t, skillDir, "v1")

	targetPath = t.TempDir()
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: targetPath}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	e = engine.New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()},
		filepath.Join(t.TempDir(), "config.toml"), filepath.Join(t.TempDir(), "manifest.json"))
	return e, targetPath, skillDir
}

func writeSkill(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: deploy\ndescription: Deploy the app\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// install puts "deploy" into "cc" through the same Preview/Apply path the TUI
// uses, so the manifest records a real installation.
func install(t *testing.T, e *engine.Engine) {
	t.Helper()
	cat := e.Refresh()
	pre := e.Preview([]reconcile.Cell{{Skill: "deploy", Source: "local", Target: "cc"}}, cat)
	rep, err := e.Apply(pre, apply.Options{})
	if err != nil || !rep.AllOK() {
		t.Fatalf("install failed: err=%v report=%+v", err, rep)
	}
}

func run(e *engine.Engine, stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(e, args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestStatusEmpty(t *testing.T) {
	e, _, _ := newEnv(t)
	code, out, _ := run(e, "", "status")
	if code != exitOK || !strings.Contains(out, "No skills installed.") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestStatusListsInstallation(t *testing.T) {
	e, _, _ := newEnv(t)
	install(t, e)
	code, out, _ := run(e, "", "status")
	if code != exitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "deploy") || !strings.Contains(out, "up-to-date") ||
		!strings.Contains(out, "1 installed") {
		t.Errorf("status output missing pieces:\n%s", out)
	}
}

func TestCheckUpToDate(t *testing.T) {
	e, _, _ := newEnv(t)
	install(t, e)
	code, out, _ := run(e, "", "check")
	if code != exitOK || !strings.Contains(out, "up to date") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestCheckReportsPendingUpdate(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2") // upstream drift

	code, out, _ := run(e, "", "check")
	if code != exitPending {
		t.Fatalf("expected exit %d, got %d\n%s", exitPending, code, out)
	}
	if !strings.Contains(out, "update available: deploy → cc (local)") {
		t.Errorf("missing update line:\n%s", out)
	}
}

func TestApplyYesReinstallsDrifted(t *testing.T) {
	e, targetPath, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2")

	code, out, _ := run(e, "", "apply", "--yes")
	if code != exitOK {
		t.Fatalf("code=%d\n%s", code, out)
	}
	if !strings.Contains(out, "reinstall") || !strings.Contains(out, "1 ok") {
		t.Errorf("apply output wrong:\n%s", out)
	}
	got, err := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if err != nil || !strings.Contains(string(got), "v2") {
		t.Errorf("installed copy not updated: err=%v content=%q", err, got)
	}
	// And the drift is gone.
	if code, _, _ := run(e, "", "check"); code != exitOK {
		t.Errorf("check should be clean after apply, got %d", code)
	}
}

func TestApplyNothingToDo(t *testing.T) {
	e, _, _ := newEnv(t)
	install(t, e)
	code, out, _ := run(e, "", "apply", "--yes")
	if code != exitOK || !strings.Contains(out, "Nothing to do.") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestApplyPromptDeclined(t *testing.T) {
	e, targetPath, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2")

	code, out, _ := run(e, "n\n", "apply")
	if code != exitPending || !strings.Contains(out, "Cancelled.") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	got, _ := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if !strings.Contains(string(got), "v1") {
		t.Errorf("declined apply must not touch the target, got %q", got)
	}
}

func TestApplyPromptAccepted(t *testing.T) {
	e, targetPath, skillDir := newEnv(t)
	install(t, e)
	writeSkill(t, skillDir, "v2")

	code, _, _ := run(e, "y\n", "apply")
	if code != exitOK {
		t.Fatalf("code=%d", code)
	}
	got, _ := os.ReadFile(filepath.Join(targetPath, "deploy", "SKILL.md"))
	if !strings.Contains(string(got), "v2") {
		t.Errorf("accepted apply should update the target, got %q", got)
	}
}

func TestApplyNeverUninstallsOrInstalls(t *testing.T) {
	e, _, skillDir := newEnv(t)
	// A second skill exists upstream but was never installed; the installed one
	// drifts. Headless apply must touch only the installed one.
	writeSkillNamed(t, filepath.Join(filepath.Dir(skillDir), "other"), "other")
	install(t, e)
	writeSkill(t, skillDir, "v2")

	code, out, _ := run(e, "", "apply", "--yes")
	if code != exitOK {
		t.Fatalf("code=%d\n%s", code, out)
	}
	if strings.Contains(out, "install other") || strings.Contains(out, "uninstall") {
		t.Errorf("headless apply must only reinstall drifted installations:\n%s", out)
	}
}

func writeSkillNamed(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: x\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRefusesFailingSource(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	// Break the source wholesale: its root vanishes.
	if err := os.RemoveAll(filepath.Dir(skillDir)); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(e, "", "apply", "--yes")
	if code != exitError {
		t.Fatalf("expected exit %d with a failing source, got %d (stderr=%q)", exitError, code, errOut)
	}
	if !strings.Contains(errOut, "refusing to apply") {
		t.Errorf("expected refusal message, got %q", errOut)
	}
}

func TestCheckFailingSourceExitsError(t *testing.T) {
	e, _, skillDir := newEnv(t)
	install(t, e)
	if err := os.RemoveAll(filepath.Dir(skillDir)); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(e, "", "check")
	if code != exitError || !strings.Contains(errOut, "source local") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestUnknownCommand(t *testing.T) {
	e, _, _ := newEnv(t)
	code, _, errOut := run(e, "", "bogus")
	if code != exitError || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestHelp(t *testing.T) {
	e, _, _ := newEnv(t)
	code, out, _ := run(e, "", "help")
	if code != exitOK || !strings.Contains(out, "Usage:") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}
