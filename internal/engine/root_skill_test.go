package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/reconcile"
)

func rootSkillGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	base := []string{
		"-c", "user.email=test@example.com",
		"-c", "user.name=Test",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestRootSkillGitHubSourceIgnoresGitDir reproduces skillmux-ia0: a GitHub
// Source whose clone root is itself a Skill (RelPath "."). Installing it must
// not copy the clone's .git into the Target, and a no-op re-fetch must leave
// the cell up-to-date rather than reporting "update available" forever.
func TestRootSkillGitHubSourceIgnoresGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	rootSkillGit(t, repo, "init")
	rootSkillGit(t, repo, "checkout", "-b", "main")
	writeSkill(t, repo, "rooty", "A root skill", "body")
	rootSkillGit(t, repo, "add", "-A")
	rootSkillGit(t, repo, "commit", "-m", "initial")

	targetPath := t.TempDir()
	cfg := &config.Config{
		Targets: []config.TargetEntry{{Name: "cc", Path: targetPath}},
		Sources: []config.SourceEntry{{Name: "gh", Kind: string(domain.SourceGitHub), Location: repo}},
	}
	e := New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()},
		filepath.Join(t.TempDir(), "config.toml"), filepath.Join(t.TempDir(), "manifest.json"))

	cat := e.Refresh()
	if len(cat.SourceErrors) != 0 {
		t.Fatalf("source errors: %v", cat.SourceErrors)
	}
	cells := []reconcile.Cell{{Skill: "rooty", Source: "gh", Target: "cc"}}
	rep, err := e.Apply(e.Preview(cells, cat), apply.Options{})
	if err != nil || !rep.AllOK() {
		t.Fatalf("Apply: %v %+v", err, rep.Results)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "rooty", "SKILL.md")); err != nil {
		t.Fatalf("skill not installed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(targetPath, "rooty", ".git")); !os.IsNotExist(err) {
		t.Errorf(".git copied into the Target (err=%v)", err)
	}

	// A no-op upstream fetch rewrites .git metadata (FETCH_HEAD etc.) but no
	// Skill content changes.
	cat = e.Refresh()
	if len(cat.SourceErrors) != 0 {
		t.Fatalf("source errors: %v", cat.SourceErrors)
	}
	if st := statusOf(e, cat, "rooty", "cc"); st != domain.StatusUpToDate {
		t.Errorf("after no-op refresh: got %q, want up-to-date", st)
	}
}
