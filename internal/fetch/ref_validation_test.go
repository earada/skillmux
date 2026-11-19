package fetch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/domain"
)

// TestFetchGitHubRejectsOptionLikeBranchOnUpdate proves a Branch that looks
// like a git option is rejected before git runs: on an existing clone the
// update path would otherwise pass it to `git fetch`, which parses
// --upload-pack=<cmd> as an option and executes <cmd>.
func TestFetchGitHubRejectsOptionLikeBranchOnUpdate(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "v1"})
	f := &Fetcher{CacheDir: t.TempDir()}
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo}
	if _, err := f.Fetch(src); err != nil {
		t.Fatalf("initial Fetch: %v", err)
	}

	marker := filepath.Join(t.TempDir(), "pwned")
	src.Branch = "--upload-pack=touch " + marker + ";git-upload-pack"
	_, err := f.Fetch(src)
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("git executed the injected --upload-pack command (marker %s created)", marker)
	}
	if err == nil || !strings.Contains(err.Error(), "invalid branch") {
		t.Fatalf("Fetch error = %v; want an invalid branch validation error", err)
	}
}

// TestFetchGitHubRejectsOptionLikeBranchOnFreshClone covers the first Fetch:
// the rejection happens before any clone is attempted.
func TestFetchGitHubRejectsOptionLikeBranchOnFreshClone(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "v1"})
	f := &Fetcher{CacheDir: t.TempDir()}
	marker := filepath.Join(t.TempDir(), "pwned")
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo,
		Branch: "--upload-pack=touch " + marker + ";git-upload-pack"}
	_, err := f.Fetch(src)
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("git executed the injected command (marker %s created)", marker)
	}
	if err == nil || !strings.Contains(err.Error(), "invalid branch") {
		t.Fatalf("Fetch error = %v; want an invalid branch validation error", err)
	}
	if isGitRepo(f.CacheDirFor(src)) {
		t.Error("no clone should be created for an invalid branch")
	}
}

// TestFetchGitHubRejectsOptionLikeLocation rejects a Location git would parse
// as an option in `git remote add/set-url`.
func TestFetchGitHubRejectsOptionLikeLocation(t *testing.T) {
	requireGit(t)
	f := &Fetcher{CacheDir: t.TempDir()}
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: "--mirror=fetch/owner/repo"}
	_, err := f.Fetch(src)
	if err == nil || !strings.Contains(err.Error(), "invalid location") {
		t.Fatalf("Fetch error = %v; want an invalid location validation error", err)
	}
}

func TestValidateRef(t *testing.T) {
	ok := []string{"", "main", "feature/x", "v1.2.3", "release-1.0", "aab6645",
		"4152bf612541cf6cc1384230c5cc035135cd9429", "HEAD"}
	for _, r := range ok {
		if err := validateRef(r); err != nil {
			t.Errorf("validateRef(%q) = %v; want nil", r, err)
		}
	}
	bad := []string{"-x", "--upload-pack=touch /tmp/pwned", "--output=/tmp/x",
		"main branch", "main\n", "ma\tin", "a\x00b", "a\x7fb"}
	for _, r := range bad {
		if err := validateRef(r); err == nil {
			t.Errorf("validateRef(%q) = nil; want an error", r)
		}
	}
}

// TestFetchGitHubAcceptsValidRefs ensures validation does not reject the
// ordinary refs: default (empty), a branch, a tag and a commit SHA.
func TestFetchGitHubAcceptsValidRefs(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "v1"})
	git(t, repo, "tag", "v1.0.0")
	git(t, repo, "branch", "feature/x")
	sha := gitOut(t, repo, "rev-parse", "HEAD")
	for _, ref := range []string{"", "main", "feature/x", "v1.0.0", sha} {
		f := &Fetcher{CacheDir: t.TempDir()}
		src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo, Branch: ref}
		// Fetch twice to exercise both fresh clone and update paths.
		for i := 0; i < 2; i++ {
			dir, err := f.Fetch(src)
			if err != nil {
				t.Fatalf("Fetch(branch %q) #%d: %v", ref, i, err)
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md")); string(got) != "v1" {
				t.Errorf("branch %q: SKILL.md = %q; want v1", ref, got)
			}
		}
	}
}
