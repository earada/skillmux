package fetch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/domain"
)

// requireGit skips a test when git is not installed. git is a hard requirement
// in production (see ADR 0006); the skip only keeps the suite green on a
// git-less developer machine.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// git runs a git command in dir and fails the test on error. Author/committer
// identity and default branch are pinned via -c so the test does not depend on
// the machine's global git config.
func git(t *testing.T, dir string, args ...string) {
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

// gitOut runs a git command in dir and returns its trimmed stdout.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// writeFile writes content to dir/rel, creating parent directories.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initRepo creates a git repository in a fresh temp dir with the given files
// committed on the default branch (main), and returns its path.
func initRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "checkout", "-b", "main")
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "initial")
	return dir
}

func TestFetchGitHubClonesDefaultBranch(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "v1"})
	f := &Fetcher{CacheDir: t.TempDir()}

	dir, err := f.Fetch(domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || string(got) != "v1" {
		t.Errorf("SKILL.md = %q, err %v; want %q", got, err, "v1")
	}
	if !isGitRepo(dir) {
		t.Error("expected the cached clone to be a git repository")
	}
}

func TestFetchGitHubClonesPinnedCommit(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "v1"})
	first := gitOut(t, repo, "rev-parse", "HEAD")
	// Advance the repo so the pin points at history, not the tip.
	writeFile(t, repo, "SKILL.md", "v2")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "v2")

	f := &Fetcher{CacheDir: t.TempDir()}
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo, Branch: first}
	dir, err := f.Fetch(src)
	if err != nil {
		t.Fatalf("Fetch pinned commit: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if string(got) != "v1" {
		t.Errorf("pinned commit should yield the v1 content, got %q", got)
	}

	rev, ok := f.Revision(src)
	if !ok || rev.ShortSHA == "" {
		t.Fatalf("revision missing for pinned commit: %+v ok=%v", rev, ok)
	}
	if rev.Ref != "" {
		t.Errorf("a SHA-pinned source should have an empty ref label, got %q", rev.Ref)
	}
	if !strings.HasPrefix(first, rev.ShortSHA) {
		t.Errorf("short sha %q should be a prefix of full %q", rev.ShortSHA, first)
	}
}

func TestLooksLikeCommitSHA(t *testing.T) {
	yes := []string{"aab6645", "4152bf612541cf6cc1384230c5cc035135cd9429"}
	no := []string{"", "main", "v1.2.3", "feature/x", "abc", "AAB6645" /* uppercase */, "release-1234567"}
	for _, s := range yes {
		if !looksLikeCommitSHA(s) {
			t.Errorf("looksLikeCommitSHA(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeCommitSHA(s) {
			t.Errorf("looksLikeCommitSHA(%q) = true, want false", s)
		}
	}
}

func TestFetchGitHubChecksOutBranch(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "main-content"})
	// A feature branch with different content.
	git(t, repo, "checkout", "-b", "feature")
	writeFile(t, repo, "SKILL.md", "feature-content")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "on feature")
	git(t, repo, "checkout", "main")

	f := &Fetcher{CacheDir: t.TempDir()}
	dir, err := f.Fetch(domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo, Branch: "feature"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || string(got) != "feature-content" {
		t.Errorf("SKILL.md = %q, err %v; want %q", got, err, "feature-content")
	}
}

func TestFetchGitHubUpdatesExistingClone(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "v1"})
	f := &Fetcher{CacheDir: t.TempDir()}

	if _, err := f.Fetch(domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo}); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	// Move the source forward, then re-fetch the same Source.
	writeFile(t, repo, "SKILL.md", "v2")
	writeFile(t, repo, "NEW.md", "added")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "v2")

	dir, err := f.Fetch(domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo})
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || string(got) != "v2" {
		t.Errorf("after update SKILL.md = %q, err %v; want %q", got, err, "v2")
	}
	if _, err := os.Stat(filepath.Join(dir, "NEW.md")); err != nil {
		t.Errorf("expected NEW.md after update: %v", err)
	}
}

func TestFetchGitHubRefetchesAfterLocationChange(t *testing.T) {
	requireGit(t)
	repoA := initRepo(t, map[string]string{"SKILL.md": "from-A"})
	repoB := initRepo(t, map[string]string{"SKILL.md": "from-B"})
	f := &Fetcher{CacheDir: t.TempDir()}

	// First fetch points the Source at repository A.
	if _, err := f.Fetch(domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repoA}); err != nil {
		t.Fatalf("Fetch A: %v", err)
	}

	// Edit the same-named Source so its Location now points at repository B and
	// refetch without clearing the cache.
	srcB := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repoB}
	dir, err := f.Fetch(srcB)
	if err != nil {
		t.Fatalf("Fetch B: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || string(got) != "from-B" {
		t.Errorf("after Location change SKILL.md = %q, err %v; want %q", got, err, "from-B")
	}

	// origin must now name repository B, and the Revision must match B's tip.
	if origin := gitOut(t, dir, "remote", "get-url", "origin"); origin != repoB {
		t.Errorf("origin = %q, want %q", origin, repoB)
	}
	rev, ok := f.Revision(srcB)
	if !ok {
		t.Fatal("expected a revision after Location change")
	}
	wantSHA := gitOut(t, repoB, "rev-parse", "--short", "HEAD")
	if rev.ShortSHA != wantSHA {
		t.Errorf("Revision.ShortSHA = %q, want repository B's tip %q", rev.ShortSHA, wantSHA)
	}
}

func TestFetchGitHubAppliesSubpath(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{
		"skills/deploy/SKILL.md": "---\nname: deploy\n---",
		"README.md":              "x",
	})
	f := &Fetcher{CacheDir: t.TempDir()}
	dir, err := f.Fetch(domain.Source{
		Name: "remote", Kind: domain.SourceGitHub, Location: repo, Subpath: "skills",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "deploy", "SKILL.md")); err != nil {
		t.Errorf("expected skills subpath content, got: %v", err)
	}
}

func TestFetchGitHubErrorsOnBadRepo(t *testing.T) {
	requireGit(t)
	f := &Fetcher{CacheDir: t.TempDir()}
	// A nonexistent local path is a valid owner/repo shape but an invalid clone.
	_, err := f.Fetch(domain.Source{
		Name: "broken", Kind: domain.SourceGitHub,
		Location: filepath.Join(t.TempDir(), "owner", "does-not-exist"),
	})
	if err == nil {
		t.Error("expected error cloning a nonexistent repository")
	}
}

func TestFetchGitHubRejectsNonRepoLocation(t *testing.T) {
	requireGit(t)
	f := &Fetcher{CacheDir: t.TempDir()}
	_, err := f.Fetch(domain.Source{Name: "bad", Kind: domain.SourceGitHub, Location: "https://github.com/owner"})
	if err == nil {
		t.Error("expected error for a Location without owner/repo")
	}
}

func TestOwnerRepo(t *testing.T) {
	cases := []struct{ in, owner, repo string }{
		{"https://github.com/owner/repo", "owner", "repo"},
		{"https://github.com/owner/repo.git", "owner", "repo"},
		{"https://github.com/owner/repo/", "owner", "repo"},
		{"git@github.com:owner/repo.git", "owner", "repo"},
	}
	for _, c := range cases {
		o, r, err := ownerRepo(c.in)
		if err != nil || o != c.owner || r != c.repo {
			t.Errorf("ownerRepo(%q) = %q,%q,%v; want %q,%q,nil", c.in, o, r, err, c.owner, c.repo)
		}
	}
	if _, _, err := ownerRepo("https://github.com/owner"); err == nil {
		t.Error("expected error for URL without owner/repo")
	}
}

func TestFetchObjectsOnlyLeavesWorkingTree(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "v1"})
	f := &Fetcher{CacheDir: t.TempDir()}
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo}

	dir, err := f.Fetch(src) // initial clone
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Advance the source.
	writeFile(t, repo, "SKILL.md", "v2")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "v2")

	// FetchObjectsOnly must NOT rewrite the working tree.
	if _, err := f.FetchObjectsOnly(src); err != nil {
		t.Fatalf("FetchObjectsOnly: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if string(got) != "v1" {
		t.Errorf("FetchObjectsOnly changed the working tree: SKILL.md = %q, want %q", got, "v1")
	}

	// A subsequent full Fetch applies the deferred checkout.
	if _, err := f.Fetch(src); err != nil {
		t.Fatalf("Fetch (checkout): %v", err)
	}
	got, _ = os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if string(got) != "v2" {
		t.Errorf("Fetch should have checked out v2; SKILL.md = %q", got)
	}
}

func TestRevisionReportsRefAndSHA(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "x"})
	f := &Fetcher{CacheDir: t.TempDir()}
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo, Branch: "main"}

	if _, err := f.Fetch(src); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	rev, ok := f.Revision(src)
	if !ok {
		t.Fatal("expected a revision for a fetched GitHub source")
	}
	if rev.Ref != "main" {
		t.Errorf("Ref = %q, want %q", rev.Ref, "main")
	}
	if rev.ShortSHA == "" {
		t.Error("ShortSHA should not be empty")
	}
}

func TestRevisionFallsBackToCheckedOutBranch(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "x"})
	f := &Fetcher{CacheDir: t.TempDir()}
	// No Branch pinned: the ref should resolve to the checked-out branch (main).
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo}

	if _, err := f.Fetch(src); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	rev, ok := f.Revision(src)
	if !ok || rev.Ref != "main" || rev.ShortSHA == "" {
		t.Fatalf("Revision = %+v ok=%v; want ref main with a sha", rev, ok)
	}
}

func TestRevisionFalseForLocalAndUnfetched(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir()}
	if _, ok := f.Revision(domain.Source{Name: "l", Kind: domain.SourceLocal, Location: t.TempDir()}); ok {
		t.Error("local source should have no revision")
	}
	if _, ok := f.Revision(domain.Source{Name: "r", Kind: domain.SourceGitHub, Location: "https://github.com/o/r"}); ok {
		t.Error("unfetched GitHub source should have no revision")
	}
}

func TestFetchLocalResolvesPathAndSubpath(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &Fetcher{CacheDir: t.TempDir()}
	dir, err := f.Fetch(domain.Source{Name: "local", Kind: domain.SourceLocal, Location: base, Subpath: "skills"})
	if err != nil {
		t.Fatalf("Fetch local: %v", err)
	}
	if dir != filepath.Join(base, "skills") {
		t.Errorf("dir = %q, want %q", dir, filepath.Join(base, "skills"))
	}
}

func TestFetchLocalErrorsWhenMissing(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir()}
	_, err := f.Fetch(domain.Source{Name: "local", Kind: domain.SourceLocal, Location: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Error("expected error for missing local source")
	}
}

func TestClearCacheRemovesGitHubSourceDir(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "x"})
	f := &Fetcher{CacheDir: t.TempDir()}
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: repo}

	dir, err := f.Fetch(src)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected cached dir to exist: %v", err)
	}

	cached, err := f.ClearCache(src)
	if err != nil {
		t.Fatalf("ClearCache: %v", err)
	}
	if !cached {
		t.Error("ClearCache(github) = false, want true")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected cache dir removed, stat err = %v", err)
	}
}

func TestClearCacheIsNoOpForLocalSource(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir()}
	cached, err := f.ClearCache(domain.Source{Name: "local", Kind: domain.SourceLocal, Location: t.TempDir()})
	if err != nil {
		t.Fatalf("ClearCache: %v", err)
	}
	if cached {
		t.Error("ClearCache(local) = true, want false (not cached)")
	}
}

// ghSrc names a GitHub Source; a helper to keep the collision tables terse.
func ghSrc(name string) domain.Source {
	return domain.Source{Name: name, Kind: domain.SourceGitHub, Location: "https://github.com/o/r"}
}

func TestCacheDirForDistinctNamesDistinctDirs(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir()}
	// Each pair once collided under the old sanitize() scheme (or would collide
	// on a case-insensitive filesystem): punctuation that mapped to underscore,
	// case-only differences, and Unicode stripped to underscore.
	pairs := [][2]string{
		{"team.one", "team/one"}, // '.' and '/' both became '_'
		{"team.one", "team_one"}, // '.' mapped onto a literal underscore
		{"a b", "a-b"},           // space vs dash after mapping
		{"Team", "team"},         // case-only (collides on case-insensitive FS)
		{"café", "cafe_"},        // Unicode stripped to underscore
		{"€", "$"},               // wholly-stripped names must still differ
	}
	for _, p := range pairs {
		a := f.CacheDirFor(ghSrc(p[0]))
		b := f.CacheDirFor(ghSrc(p[1]))
		if a == b {
			t.Errorf("CacheDirFor(%q) and CacheDirFor(%q) collide: both %q", p[0], p[1], a)
		}
		// Distinct even when compared case-insensitively (case-insensitive FS).
		if strings.EqualFold(filepath.Base(a), filepath.Base(b)) {
			t.Errorf("CacheDirFor(%q)=%q and CacheDirFor(%q)=%q collide case-insensitively", p[0], a, p[1], b)
		}
	}
}

func TestCacheDirForIsStable(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir()}
	src := ghSrc("team.one")
	if a, b := f.CacheDirFor(src), f.CacheDirFor(src); a != b {
		t.Errorf("CacheDirFor not stable: %q != %q", a, b)
	}
}

func TestCacheDirForReadableSlug(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir()}
	// The directory keeps a recognisable prefix from the name for humans.
	dir := filepath.Base(f.CacheDirFor(ghSrc("team.one")))
	if !strings.HasPrefix(dir, "team_one-") {
		t.Errorf("cache dir %q missing readable %q prefix", dir, "team_one-")
	}
	// An empty name still yields a non-empty, separator-safe directory.
	empty := filepath.Base(f.CacheDirFor(ghSrc("")))
	if !strings.HasPrefix(empty, "src-") {
		t.Errorf("empty name cache dir %q, want %q prefix", empty, "src-")
	}
}

func TestFetchGitHubDistinctNamesDoNotShareClone(t *testing.T) {
	requireGit(t)
	repoA := initRepo(t, map[string]string{"SKILL.md": "from-A"})
	repoB := initRepo(t, map[string]string{"SKILL.md": "from-B"})
	f := &Fetcher{CacheDir: t.TempDir()}

	// team.one and team/one are distinct Config names that once shared a clone
	// dir; each must now get its own repository content.
	dirA, err := f.Fetch(domain.Source{Name: "team.one", Kind: domain.SourceGitHub, Location: repoA})
	if err != nil {
		t.Fatalf("Fetch A: %v", err)
	}
	dirB, err := f.Fetch(domain.Source{Name: "team/one", Kind: domain.SourceGitHub, Location: repoB})
	if err != nil {
		t.Fatalf("Fetch B: %v", err)
	}
	if dirA == dirB {
		t.Fatalf("distinct Sources shared clone dir %q", dirA)
	}
	if got, _ := os.ReadFile(filepath.Join(dirA, "SKILL.md")); string(got) != "from-A" {
		t.Errorf("team.one SKILL.md = %q, want from-A", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dirB, "SKILL.md")); string(got) != "from-B" {
		t.Errorf("team/one SKILL.md = %q, want from-B", got)
	}
}

func TestFetchGitHubInvalidatesLegacyCacheDir(t *testing.T) {
	requireGit(t)
	repo := initRepo(t, map[string]string{"SKILL.md": "fresh"})
	f := &Fetcher{CacheDir: t.TempDir()}
	src := domain.Source{Name: "team.one", Kind: domain.SourceGitHub, Location: repo}

	// Simulate an older skillmux having cloned under the legacy sanitize() path.
	legacy := f.legacyCacheDirFor(src)
	if err := os.MkdirAll(filepath.Join(legacy, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, legacy, "SKILL.md", "stale")

	dir, err := f.Fetch(src)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// The fresh clone lands at the new collision-free path with current content.
	if dir == legacy {
		t.Fatalf("fetch reused legacy path %q", legacy)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md")); string(got) != "fresh" {
		t.Errorf("SKILL.md = %q, want fresh", got)
	}
	// The stale legacy directory is cleaned up rather than left to leak disk.
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy cache dir not invalidated, stat err = %v", err)
	}
}

func TestFetchGitHubRequiresGit(t *testing.T) {
	// With an empty PATH, exec.LookPath("git") fails and the fetch must report
	// the missing-git requirement rather than attempting a clone.
	t.Setenv("PATH", "")
	f := &Fetcher{CacheDir: t.TempDir()}
	_, err := f.Fetch(domain.Source{Name: "r", Kind: domain.SourceGitHub, Location: "https://github.com/o/r"})
	if err == nil {
		t.Fatal("expected error when git is unavailable")
	}
}
