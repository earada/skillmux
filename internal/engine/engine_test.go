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

	rep, err := e.Apply(cell(), cat, apply.Options{})
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
	if _, err := e.Apply(cell(), cat, apply.Options{}); err != nil {
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
	if _, err := e.Apply(cell(), cat, apply.Options{}); err != nil {
		t.Fatal(err)
	}
	cat = e.Refresh()
	if st := statusOf(e, cat, "deploy", "cc"); st != domain.StatusUpToDate {
		t.Errorf("after reinstall: got %q, want up-to-date", st)
	}
}

func TestApplyEmptyDesiredUninstalls(t *testing.T) {
	e, targetPath, _, _ := newEnv(t)
	cat := e.Refresh()
	if _, err := e.Apply(cell(), cat, apply.Options{}); err != nil {
		t.Fatal(err)
	}
	// Now deselect everything.
	if _, err := e.Apply(nil, cat, apply.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "deploy")); !os.IsNotExist(err) {
		t.Error("skill should have been uninstalled")
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
