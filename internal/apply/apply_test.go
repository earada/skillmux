package apply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/fingerprint"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

// makeSource creates a cached skill folder with one file and returns its dir.
func makeSource(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readInstalled(t *testing.T, targetPath, skill string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(targetPath, skill, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading installed skill: %v", err)
	}
	return string(b)
}

func TestInstallCopiesAndRecords(t *testing.T) {
	src := makeSource(t, "v1")
	targetPath := t.TempDir()
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp1"}},
		man, Options{},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "v1" {
		t.Errorf("installed content = %q, want v1", got)
	}
	in, ok := man.Find("t", "deploy")
	if !ok || in.Fingerprint != "fp1" || in.SourceName != "s" {
		t.Errorf("manifest entry wrong: %+v ok=%v", in, ok)
	}
}

// TestInstalledSkillMatchesFingerprint proves the invariant behind skillmux-iot:
// a successfully installed Skill contains the exact defining SKILL.md that its
// recorded fingerprint covers. It fingerprints the source folder, installs it,
// then checks the installed SKILL.md is present with identical bytes and that
// re-fingerprinting the installed folder reproduces the recorded value — so the
// file that defines the Skill is neither dropped by the copy nor omitted from
// the fingerprint.
func TestInstalledSkillMatchesFingerprint(t *testing.T) {
	const body = "---\nname: deploy\ndescription: d\n---\nbody\n"
	src := makeSource(t, body)
	// A second regular file, to ensure the whole folder round-trips.
	if err := os.WriteFile(filepath.Join(src, "extra.txt"), []byte("more"), 0o644); err != nil {
		t.Fatal(err)
	}

	fp, err := fingerprint.Dir(src)
	if err != nil {
		t.Fatalf("fingerprint source: %v", err)
	}

	targetPath := t.TempDir()
	man := &manifest.Manifest{}
	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: fp}},
		man, Options{},
	)
	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}

	if got := readInstalled(t, targetPath, "deploy"); got != body {
		t.Errorf("installed SKILL.md = %q, want %q", got, body)
	}
	installedDir := filepath.Join(targetPath, "deploy")
	got, err := fingerprint.Dir(installedDir)
	if err != nil {
		t.Fatalf("fingerprint installed: %v", err)
	}
	if got != fp {
		t.Errorf("installed fingerprint %q != recorded %q; installed content diverged from the fingerprinted source", got, fp)
	}
}

func TestReinstallOverwritesTrackedFolder(t *testing.T) {
	src := makeSource(t, "v2")
	targetPath := t.TempDir()
	// Pre-existing tracked install with old content.
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("v1"), 0o644)
	os.WriteFile(filepath.Join(targetPath, "deploy", "stale.txt"), []byte("x"), 0o644)
	man := &manifest.Manifest{}
	man.Put(installation("deploy", "t", "s", "old"))

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Reinstall, SkillName: "deploy", SourceName: "s", TargetName: "t", Reason: reconcile.ReasonUpdateAvailable},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp2"}},
		man, Options{},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "v2" {
		t.Errorf("content = %q, want v2", got)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy", "stale.txt")); !os.IsNotExist(err) {
		t.Error("stale file from previous install should be gone after reinstall")
	}
	if in, _ := man.Find("t", "deploy"); in.Fingerprint != "fp2" {
		t.Errorf("fingerprint not updated: %+v", in)
	}
}

func TestReinstallAfterTargetPathMoveMigratesAndClearsOldPath(t *testing.T) {
	src := makeSource(t, "v1")
	oldPath := t.TempDir()
	newPath := t.TempDir()

	// A prior install lives at oldPath and is tracked there.
	if err := os.MkdirAll(filepath.Join(oldPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(oldPath, "deploy", "SKILL.md"), []byte("v1"), 0o644)
	man := &manifest.Manifest{}
	man.Put(domain.Installation{SkillName: "deploy", TargetName: "t", SourceName: "s", Path: oldPath, Fingerprint: "fp1"})

	// The Target "t" now points at newPath; a target-moved reinstall runs.
	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Reinstall, SkillName: "deploy", SourceName: "s", TargetName: "t", Reason: reconcile.ReasonTargetMoved},
		}},
		map[string]string{"t": newPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp1"}},
		man, Options{},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	// Installed at the new path.
	if got := readInstalled(t, newPath, "deploy"); got != "v1" {
		t.Errorf("content at new path = %q, want v1", got)
	}
	// Old path cleaned up — no orphaned copy left behind.
	if _, err := os.Stat(filepath.Join(oldPath, "deploy")); !os.IsNotExist(err) {
		t.Error("stale folder at old path should have been removed")
	}
	// Manifest now records the new path.
	if in, _ := man.Find("t", "deploy"); in.Path != newPath {
		t.Errorf("manifest path = %q, want %q", in.Path, newPath)
	}
}

func TestUninstallRemovesFolderAndEntry(t *testing.T) {
	targetPath := t.TempDir()
	os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755)
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("v1"), 0o644)
	man := &manifest.Manifest{}
	man.Put(installation("deploy", "t", "s", "fp"))

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Uninstall, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		nil, man, Options{},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success, got %+v", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Error("folder should be removed")
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Error("manifest entry should be removed")
	}
}

func TestInstallRefusesUntrackedFolderByDefault(t *testing.T) {
	src := makeSource(t, "new")
	targetPath := t.TempDir()
	// An untracked folder placed by hand.
	os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755)
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("handmade"), 0o644)
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected refusal to overwrite untracked folder")
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "handmade" {
		t.Errorf("untracked folder was modified: %q", got)
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Error("nothing should be recorded for a refused install")
	}
}

func TestInstallOverwritesUntrackedWhenConfirmed(t *testing.T) {
	src := makeSource(t, "new")
	targetPath := t.TempDir()
	os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755)
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("handmade"), 0o644)
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp"}},
		man, Options{ConfirmOverwrite: func(_, _, _ string) bool { return true }},
	)

	if !rep.AllOK() {
		t.Fatalf("expected success with confirm, got %+v", rep.Results)
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "new" {
		t.Errorf("content = %q, want new", got)
	}
}

func TestBestEffortContinuesAfterFailure(t *testing.T) {
	src := makeSource(t, "ok")
	targetPath := t.TempDir()
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			// Fails: no resolved skill provided for "missing".
			{Kind: reconcile.Install, SkillName: "missing", SourceName: "s", TargetName: "t"},
			// Succeeds.
			{Kind: reconcile.Install, SkillName: "good", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "good"}: {Dir: src, Fingerprint: "fp"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected one failure")
	}
	if _, ok := man.Find("t", "good"); !ok {
		t.Error("the good install should have proceeded despite the earlier failure")
	}
}

func TestInstallRefusesNameThatEscapesTarget(t *testing.T) {
	// Defensive backstop (skillmux-aps): even if a malformed name reached Apply,
	// an install must not create anything outside the Target.
	src := makeSource(t, "payload")
	parent := t.TempDir()
	targetPath := filepath.Join(parent, "target")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "../victim", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "../victim"}: {Dir: src, Fingerprint: "fp"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected refusal for name escaping the target")
	}
	if _, err := os.Stat(filepath.Join(parent, "victim")); !os.IsNotExist(err) {
		t.Error("install must not create a sibling outside the target")
	}
	if _, ok := man.Find("t", "../victim"); ok {
		t.Error("nothing should be recorded for a refused install")
	}
}

func TestUninstallRefusesNameThatEscapesTarget(t *testing.T) {
	// A source-controlled name must not let uninstall RemoveAll a sibling path.
	parent := t.TempDir()
	targetPath := filepath.Join(parent, "target")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(parent, "victim")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(victim, "keep.txt"), []byte("precious"), 0o644)
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Uninstall, SkillName: "../victim", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		nil, man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected refusal for name escaping the target")
	}
	if _, err := os.Stat(filepath.Join(victim, "keep.txt")); err != nil {
		t.Errorf("uninstall must not touch a sibling outside the target: %v", err)
	}
}

func TestUninstallRefusesNameResolvingToTargetItself(t *testing.T) {
	// A "." name resolves to the Target itself; RemoveAll on it would wipe the
	// whole Target. destWithin must reject it.
	targetPath := t.TempDir()
	os.WriteFile(filepath.Join(targetPath, "keep.txt"), []byte("precious"), 0o644)
	man := &manifest.Manifest{}

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Uninstall, SkillName: ".", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		nil, man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected refusal for name resolving to the target itself")
	}
	if _, err := os.Stat(filepath.Join(targetPath, "keep.txt")); err != nil {
		t.Errorf("uninstall must not clear the target itself: %v", err)
	}
}

func TestFailedInstallLeavesUntrackedDestinationUntouched(t *testing.T) {
	// Fault injection: the copy fails partway (e.g. a source file becomes
	// unreadable). A brand-new install must not leave a partial folder behind
	// and must record nothing.
	src := makeSource(t, "new")
	targetPath := t.TempDir()
	man := &manifest.Manifest{}

	restore := copyTree
	copyTree = func(_, _ string) error { return errors.New("boom: read failed mid-copy") }
	defer func() { copyTree = restore }()

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Install, SkillName: "deploy", SourceName: "s", TargetName: "t"},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected the install to fail")
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Error("a failed install must not leave a destination folder behind")
	}
	if _, ok := man.Find("t", "deploy"); ok {
		t.Error("nothing should be recorded for a failed install")
	}
	// No staging leftovers.
	assertNoStagingLeftovers(t, targetPath)
}

func TestFailedReinstallPreservesPriorInstallationAndManifest(t *testing.T) {
	// Fault injection on read: the reinstall copy fails. The prior working
	// installation and its Manifest entry must survive intact.
	src := makeSource(t, "v2")
	targetPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("v1"), 0o644)
	man := &manifest.Manifest{}
	man.Put(installation("deploy", "t", "s", "fp1"))

	restore := copyTree
	copyTree = func(_, _ string) error { return errors.New("boom: read failed mid-copy") }
	defer func() { copyTree = restore }()

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Reinstall, SkillName: "deploy", SourceName: "s", TargetName: "t", Reason: reconcile.ReasonUpdateAvailable},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp2"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected the reinstall to fail")
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "v1" {
		t.Errorf("prior installation clobbered: content = %q, want v1", got)
	}
	if in, _ := man.Find("t", "deploy"); in.Fingerprint != "fp1" {
		t.Errorf("manifest entry should be preserved on failure: %+v", in)
	}
	assertNoStagingLeftovers(t, targetPath)
}

func TestFailedReinstallRollsBackOnRenameFailure(t *testing.T) {
	// Fault injection on rename: the copy completes and the prior install is
	// moved aside, but the final swap fails. The rollback must restore the
	// prior installation and Manifest entry, with no staging leftovers.
	src := makeSource(t, "v2")
	targetPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(targetPath, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(targetPath, "deploy", "SKILL.md"), []byte("v1"), 0o644)
	man := &manifest.Manifest{}
	man.Put(installation("deploy", "t", "s", "fp1"))

	// Let the "move prior installation aside" rename succeed, then fail the
	// "swap staged copy into place" rename to exercise the rollback path.
	restore := renamePath
	calls := 0
	renamePath = func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return errors.New("boom: rename failed on swap")
		}
		return os.Rename(oldPath, newPath)
	}
	defer func() { renamePath = restore }()

	rep := Apply(
		reconcile.Plan{Operations: []reconcile.Operation{
			{Kind: reconcile.Reinstall, SkillName: "deploy", SourceName: "s", TargetName: "t", Reason: reconcile.ReasonUpdateAvailable},
		}},
		map[string]string{"t": targetPath},
		map[SkillID]ResolvedSkill{{Source: "s", Skill: "deploy"}: {Dir: src, Fingerprint: "fp2"}},
		man, Options{},
	)

	if rep.AllOK() {
		t.Fatal("expected the reinstall to fail on the swap")
	}
	if got := readInstalled(t, targetPath, "deploy"); got != "v1" {
		t.Errorf("rollback failed: content = %q, want v1", got)
	}
	if in, _ := man.Find("t", "deploy"); in.Fingerprint != "fp1" {
		t.Errorf("manifest entry should be preserved after rollback: %+v", in)
	}
	assertNoStagingLeftovers(t, targetPath)
}

// assertNoStagingLeftovers verifies the swap machinery cleaned up its temp
// folders under targetPath, whatever the outcome.
func assertNoStagingLeftovers(t *testing.T, targetPath string) {
	t.Helper()
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		t.Fatalf("reading target dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".skillmux-stage-") {
			t.Errorf("staging leftover not cleaned up: %s", e.Name())
		}
	}
}

func installation(skill, target, source, fp string) domain.Installation {
	return domain.Installation{SkillName: skill, TargetName: target, SourceName: source, Fingerprint: fp}
}
