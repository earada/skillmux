package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

func writeSkill(t *testing.T, dir, name, desc, extra string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newEnv builds an Engine over a single local Source containing one skill
// ("deploy") and a single Target, with an empty in-memory manifest persisted to
// a temp path.
func newEnv(t *testing.T) (*Engine, string /*targetPath*/, string /*srcSkillDir*/, string /*manifestPath*/) {
	t.Helper()
	srcRoot := t.TempDir()
	skillDir := filepath.Join(srcRoot, "deploy")
	writeSkill(t, skillDir, "deploy", "Deploy the app", "v1")

	targetPath := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")

	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: targetPath}},
		Sources: []config.SourceEntry{{Name: "local", Location: srcRoot}},
	}
	man := &manifest.Manifest{}
	f := &fetch.Fetcher{CacheDir: t.TempDir()}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	return New(cfg, man, f, configPath, manifestPath), targetPath, skillDir, manifestPath
}

func cell() []reconcile.Cell {
	return []reconcile.Cell{{Skill: "deploy", Source: "local", Target: "cc"}}
}

func TestRefreshListsAvailableSkills(t *testing.T) {
	e, _, _, _ := newEnv(t)
	cat := e.Refresh()
	if len(cat.SourceErrors) != 0 {
		t.Fatalf("unexpected source errors: %v", cat.SourceErrors)
	}
	if len(cat.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %+v", cat.Skills)
	}
	s := cat.Skills[0]
	if s.Name != "deploy" || s.Source != "local" || s.Description != "Deploy the app" {
		t.Errorf("skill metadata wrong: %+v", s)
	}
	if s.Fingerprint == "" || s.Dir == "" {
		t.Errorf("skill missing fingerprint/dir: %+v", s)
	}
}

func TestStatusNotInstalledThenUpToDate(t *testing.T) {
	e, targetPath, _, manifestPath := newEnv(t)
	cat := e.Refresh()

	st := statusOf(e, cat, "deploy", "cc")
	if st != domain.StatusNotInstalled {
		t.Fatalf("before install: got %q, want not-installed", st)
	}

	rep, err := e.Apply(e.Preview(cell(), cat), apply.Options{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !rep.AllOK() {
		t.Fatalf("apply failed: %+v", rep.Results)
	}
	// Skill copied to the target.
	if _, err := os.Stat(filepath.Join(targetPath, "deploy", "SKILL.md")); err != nil {
		t.Errorf("skill not installed on disk: %v", err)
	}
	// Manifest persisted.
	persisted, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted.Find("cc", "deploy"); !ok {
		t.Error("manifest not persisted to disk")
	}

	// Status now up-to-date.
	cat = e.Refresh()
	if st := statusOf(e, cat, "deploy", "cc"); st != domain.StatusUpToDate {
		t.Errorf("after install: got %q, want up-to-date", st)
	}
}

func TestStatusUpdateAvailableAfterSourceChanges(t *testing.T) {
	e, _, srcSkillDir, _ := newEnv(t)
	cat := e.Refresh()
	if _, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil {
		t.Fatal(err)
	}

	// Upstream change.
	if err := os.WriteFile(filepath.Join(srcSkillDir, "SKILL.md"), []byte("---\nname: deploy\ndescription: Deploy the app\n---\nv2-changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat = e.Refresh()
	if st := statusOf(e, cat, "deploy", "cc"); st != domain.StatusUpdateAvailable {
		t.Fatalf("after upstream change: got %q, want update-available", st)
	}

	// Reinstall brings it up to date.
	if _, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil {
		t.Fatal(err)
	}
	cat = e.Refresh()
	if st := statusOf(e, cat, "deploy", "cc"); st != domain.StatusUpToDate {
		t.Errorf("after reinstall: got %q, want up-to-date", st)
	}
}

// TestRefreshRetainsCachedSkillsOnSourceFailure exercises skillmux-ewq: a
// successful refresh followed by a failing one must keep the Source's
// last-known-good entries (and their fingerprints) instead of erasing them,
// while still surfacing the current error.
func TestRefreshRetainsCachedSkillsOnSourceFailure(t *testing.T) {
	e, _, srcSkillDir, _ := newEnv(t)

	fresh := e.Refresh()
	if len(fresh.Skills) != 1 || len(fresh.SourceErrors) != 0 {
		t.Fatalf("first refresh: skills=%+v errors=%v", fresh.Skills, fresh.SourceErrors)
	}
	wantFP := fresh.Skills[0].Fingerprint

	// Make the Source unavailable and refresh again.
	if err := os.RemoveAll(filepath.Dir(srcSkillDir)); err != nil {
		t.Fatal(err)
	}
	after := e.Refresh()

	if _, ok := after.SourceErrors["local"]; !ok {
		t.Fatalf("expected an error for the unavailable source, got %v", after.SourceErrors)
	}
	if len(after.Skills) != 1 {
		t.Fatalf("expected 1 retained skill, got %+v", after.Skills)
	}
	if after.Skills[0].Name != "deploy" || after.Skills[0].Fingerprint != wantFP {
		t.Errorf("retained skill wrong: %+v (want fingerprint %q)", after.Skills[0], wantFP)
	}

	// The cache must not be clobbered with the empty failure snapshot.
	if cached := e.CachedCatalog(); len(cached.Skills) != 1 || cached.Skills[0].Name != "deploy" {
		t.Fatalf("cache overwritten by failure snapshot: %+v", cached.Skills)
	}
}

// TestOfflineRefreshKeepsInstalledSkillAvailable exercises the offline-startup
// half of skillmux-ewq: a fresh Engine over an existing Manifest installation
// whose Source is unavailable must still see the installed Skill (from the
// cache) after a failing Refresh, so a Preview that keeps it selected plans no
// operations — in particular, no spurious uninstall.
func TestOfflineRefreshKeepsInstalledSkillAvailable(t *testing.T) {
	e, _, srcSkillDir, manifestPath := newEnv(t)

	// Install the skill from a working Source, populating the catalog cache.
	cat := e.Refresh()
	if _, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil {
		t.Fatal(err)
	}

	// Simulate a fresh offline startup: a new Engine sharing the cache dir and
	// the persisted manifest, with the Source now unavailable.
	if err := os.RemoveAll(filepath.Dir(srcSkillDir)); err != nil {
		t.Fatal(err)
	}
	man, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	e2 := New(e.Config, man, &fetch.Fetcher{CacheDir: e.Fetcher.CacheDir}, "", manifestPath)

	offline := e2.Refresh()
	if _, ok := offline.SourceErrors["local"]; !ok {
		t.Fatalf("expected offline source error, got %v", offline.SourceErrors)
	}
	if len(offline.Skills) != 1 {
		t.Fatalf("offline refresh dropped installed skill: %+v", offline.Skills)
	}
	if st := statusOf(e2, offline, "deploy", "cc"); st != domain.StatusUpToDate {
		t.Errorf("offline status: got %q, want up-to-date", st)
	}
	pre := e2.Preview(cell(), offline)
	if len(pre.Plan.Operations) != 0 {
		t.Fatalf("expected no operations for a retained installation, got %+v", pre.Plan.Operations)
	}
}

func TestStatusAndPreviewReactToTargetPathEdit(t *testing.T) {
	e, oldPath, _, _ := newEnv(t)
	cat := e.Refresh()

	// Install into target "cc" at its original path.
	if _, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil {
		t.Fatal(err)
	}
	cat = e.Refresh()
	if st := statusOf(e, cat, "deploy", "cc"); st != domain.StatusUpToDate {
		t.Fatalf("after install: got %q, want up-to-date", st)
	}

	// Edit the Target's path (name unchanged) — the classic bug: the manifest
	// fingerprint still matches, but the files sit at oldPath and newPath is
	// empty.
	newPath := t.TempDir()
	e.Config.Targets[0].Path = newPath

	// Status must NOT report up-to-date solely from the stale manifest entry.
	cat = e.Refresh()
	if st := statusOf(e, cat, "deploy", "cc"); st != domain.StatusUpdateAvailable {
		t.Fatalf("after path edit: got %q, want update-available", st)
	}

	// Preview must emit an operation (previously it emitted none).
	pre := e.Preview(cell(), cat)
	if len(pre.Plan.Operations) != 1 || pre.Plan.Operations[0].Reason != reconcile.ReasonTargetMoved {
		t.Fatalf("expected one target-moved reinstall, got %+v", pre.Plan.Operations)
	}

	// Applying it migrates to the new path and clears the old one.
	if _, err := e.Apply(pre, apply.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(newPath, "deploy", "SKILL.md")); err != nil {
		t.Errorf("skill not installed at new path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(oldPath, "deploy")); !os.IsNotExist(err) {
		t.Error("skill should have been removed from the old path")
	}
	cat = e.Refresh()
	if st := statusOf(e, cat, "deploy", "cc"); st != domain.StatusUpToDate {
		t.Errorf("after migration: got %q, want up-to-date", st)
	}
}

func TestApplyEmptyDesiredUninstalls(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	cat := e.Refresh()
	if _, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil {
		t.Fatal(err)
	}
	// Now deselect everything.
	if _, err := e.Apply(e.Preview(nil, cat), apply.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Error("skill should have been uninstalled")
	}
}

func TestUpstreamRemovalKeepsInstalledReconcilable(t *testing.T) {
	// Install "deploy", then remove it from its Source (it disappears upstream).
	e, targetPath, skillDir, _ := newEnv(t)
	cat := e.Refresh()
	if _, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := os.RemoveAll(skillDir); err != nil {
		t.Fatal(err)
	}
	cat = e.Refresh() // catalog no longer offers deploy
	if len(cat.Skills) != 0 {
		t.Fatalf("expected empty catalog after removal, got %+v", cat.Skills)
	}

	// The installed row is surfaced as a last-known, unavailable row.
	un := e.UnavailableSkills(cat)
	if len(un) != 1 || un[0].Name != "deploy" || !un[0].Unavailable {
		t.Fatalf("UnavailableSkills = %+v, want one unavailable deploy", un)
	}
	// Status reports it as unavailable rather than an installed state that would
	// imply a reinstall.
	var got domain.Status
	for _, c := range e.Status(cat) {
		if c.SkillName == "deploy" && c.TargetName == "cc" {
			got = c.Status
		}
	}
	if got != domain.StatusUnavailable {
		t.Fatalf("status = %q, want unavailable", got)
	}

	// Kept (still desired): Preview must not emit a doomed Reinstall, and Apply
	// must be a no-op that leaves the files in place.
	pre := e.Preview(cell(), cat)
	if len(pre.Plan.Operations) != 0 {
		t.Fatalf("keeping an unavailable skill should be a no-op, got %+v", pre.Plan.Operations)
	}
	if _, err := e.Apply(pre, apply.Options{}); err != nil {
		t.Fatalf("apply keep: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); err != nil {
		t.Errorf("kept skill should still be installed: %v", err)
	}

	// Deselected: it uninstalls cleanly even though its Source is gone.
	if _, err := e.Apply(e.Preview(nil, cat), apply.Options{}); err != nil {
		t.Fatalf("apply uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Error("deselected unavailable skill should have been uninstalled")
	}
}

func TestRefreshCapturesSourceErrors(t *testing.T) {
	cfg := &config.Config{
		Sources: []config.SourceEntry{{Name: "broken", Location: filepath.Join(t.TempDir(), "does-not-exist")}},
	}
	e := New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))
	cat := e.Refresh()
	if _, ok := cat.SourceErrors["broken"]; !ok {
		t.Errorf("expected a source error for 'broken', got %v", cat.SourceErrors)
	}
}

func TestViewDeferralBookkeeping(t *testing.T) {
	e := New(&config.Config{}, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", "")

	// Opening a view marks the source; closing it clears the mark.
	e.BeginView("remote")
	if e.ViewedSource() != "remote" {
		t.Fatalf("ViewedSource = %q, want %q", e.ViewedSource(), "remote")
	}
	if e.EndView() {
		t.Error("EndView should be false when no checkout was deferred")
	}
	if e.ViewedSource() != "" {
		t.Errorf("ViewedSource not cleared: %q", e.ViewedSource())
	}

	// When a Refresh deferred the checkout, EndView reports it once, then clears.
	e.BeginView("remote")
	e.deferred["remote"] = true // a Refresh would set this while the view is open
	if !e.EndView() {
		t.Error("EndView should report the deferred checkout")
	}
	if e.EndView() {
		t.Error("the deferred flag should have been cleared after the first EndView")
	}
}

func statusOf(e *Engine, cat Catalog, skill, target string) domain.Status {
	for _, c := range e.Status(cat) {
		if c.SkillName == skill && c.TargetName == target {
			return c.Status
		}
	}
	return ""
}
