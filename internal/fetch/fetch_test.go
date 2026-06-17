package fetch

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/domain"
)

func TestTarballURL(t *testing.T) {
	cases := []struct {
		repo, branch, want string
	}{
		{"https://github.com/owner/repo", "main", "https://codeload.github.com/owner/repo/tar.gz/main"},
		{"https://github.com/owner/repo", "", "https://codeload.github.com/owner/repo/tar.gz/HEAD"},
		{"https://github.com/owner/repo.git", "", "https://codeload.github.com/owner/repo/tar.gz/HEAD"},
		{"https://github.com/owner/repo/", "v1.2.3", "https://codeload.github.com/owner/repo/tar.gz/v1.2.3"},
		{"git@github.com:owner/repo.git", "main", "https://codeload.github.com/owner/repo/tar.gz/main"},
	}
	for _, c := range cases {
		got, err := TarballURL(c.repo, c.branch)
		if err != nil {
			t.Errorf("TarballURL(%q,%q): %v", c.repo, c.branch, err)
			continue
		}
		if got != c.want {
			t.Errorf("TarballURL(%q,%q) = %q, want %q", c.repo, c.branch, got, c.want)
		}
	}
}

func TestTarballURLRejectsNonRepo(t *testing.T) {
	if _, err := TarballURL("https://github.com/owner", ""); err == nil {
		t.Error("expected error for URL without owner/repo")
	}
}

// makeTarGz builds a gzipped tar where every path is under a single top-level
// directory (as GitHub archives are), from a name->content map.
func makeTarGz(t *testing.T, topDir string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		full := topDir + "/" + name
		if err := tw.WriteHeader(&tar.Header{Name: full, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractTarGzStripsTopDir(t *testing.T) {
	dest := t.TempDir()
	data := makeTarGz(t, "repo-main", map[string]string{
		"SKILL.md":         "v1",
		"scripts/run.sh":   "echo hi",
		"nested/a/b/c.txt": "deep",
	})
	if err := extractTarGz(bytes.NewReader(data), dest); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil || string(got) != "v1" {
		t.Errorf("SKILL.md not extracted at top level: %v %q", err, got)
	}
	if _, err := os.Stat(filepath.Join(dest, "scripts", "run.sh")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "repo-main")); !os.IsNotExist(err) {
		t.Error("top-level archive directory should have been stripped")
	}
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	data := makeTarGz(t, "repo-main", map[string]string{"../escape.txt": "evil"})
	if err := extractTarGz(bytes.NewReader(data), dest); err == nil {
		t.Error("expected error on path traversal")
	}
}

// stubTransport returns the same response for any request, recording the last
// request it saw so tests can assert on the URL and headers.
type stubTransport struct {
	status int
	body   []byte
	last   *http.Request
}

func (s *stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.last = r
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(bytes.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func tokenFunc(tok string) func() (string, error) {
	return func() (string, error) { return tok, nil }
}

func TestApiTarballURL(t *testing.T) {
	cases := []struct {
		repo, branch, want string
	}{
		{"https://github.com/owner/repo", "main", "https://api.github.com/repos/owner/repo/tarball/main"},
		{"https://github.com/owner/repo", "", "https://api.github.com/repos/owner/repo/tarball"},
		{"git@github.com:owner/repo.git", "v1", "https://api.github.com/repos/owner/repo/tarball/v1"},
	}
	for _, c := range cases {
		got, err := apiTarballURL(c.repo, c.branch)
		if err != nil {
			t.Errorf("apiTarballURL(%q,%q): %v", c.repo, c.branch, err)
			continue
		}
		if got != c.want {
			t.Errorf("apiTarballURL(%q,%q) = %q, want %q", c.repo, c.branch, got, c.want)
		}
	}
}

func TestFetchGitHubWithTokenUsesAuthenticatedAPI(t *testing.T) {
	tarball := makeTarGz(t, "repo-main", map[string]string{"SKILL.md": "x"})
	tr := &stubTransport{status: 200, body: tarball}
	f := &Fetcher{
		Client:   &http.Client{Transport: tr},
		CacheDir: t.TempDir(),
		Token:    tokenFunc("sekret-token"),
	}
	if _, err := f.Fetch(domain.Source{
		Name: "remote", Kind: domain.SourceGitHub,
		Location: "https://github.com/owner/repo", Branch: "main",
	}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := tr.last.URL.String(), "https://api.github.com/repos/owner/repo/tarball/main"; got != want {
		t.Errorf("request URL = %q, want %q", got, want)
	}
	if got := tr.last.Header.Get("Authorization"); got != "Bearer sekret-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sekret-token")
	}
}

func TestFetchGitHubWithoutTokenStaysAnonymous(t *testing.T) {
	tarball := makeTarGz(t, "repo-main", map[string]string{"SKILL.md": "x"})
	tr := &stubTransport{status: 200, body: tarball}
	f := &Fetcher{
		Client:   &http.Client{Transport: tr},
		CacheDir: t.TempDir(),
		Token:    tokenFunc(""),
	}
	if _, err := f.Fetch(domain.Source{
		Name: "remote", Kind: domain.SourceGitHub,
		Location: "https://github.com/owner/repo", Branch: "main",
	}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := tr.last.URL.String(), "https://codeload.github.com/owner/repo/tar.gz/main"; got != want {
		t.Errorf("request URL = %q, want %q", got, want)
	}
	if got := tr.last.Header.Get("Authorization"); got != "" {
		t.Errorf("anonymous fetch sent Authorization = %q, want none", got)
	}
}

// A failed fetch must never echo the token in its error.
func TestFetchGitHubErrorDoesNotLeakToken(t *testing.T) {
	const secret = "super-secret-token-value"
	tr := &stubTransport{status: 401, body: []byte("Bad credentials")}
	f := &Fetcher{
		Client:   &http.Client{Transport: tr},
		CacheDir: t.TempDir(),
		Token:    tokenFunc(secret),
	}
	_, err := f.Fetch(domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: "https://github.com/o/r"})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaked token: %q", err.Error())
	}
}

func TestGithubStatusErrorHints(t *testing.T) {
	withTok := githubStatusError("r", 404, true).Error()
	if strings.Contains(withTok, "GH_TOKEN") {
		t.Errorf("404-with-token should not push the user to set a token: %q", withTok)
	}
	noTok := githubStatusError("r", 404, false).Error()
	if !strings.Contains(noTok, "GH_TOKEN") {
		t.Errorf("404-without-token should mention GH_TOKEN: %q", noTok)
	}
}

func TestFetchGitHubDownloadsExtractsAndAppliesSubpath(t *testing.T) {
	tarball := makeTarGz(t, "repo-main", map[string]string{
		"skills/deploy/SKILL.md": "---\nname: deploy\n---",
		"README.md":              "x",
	})
	f := &Fetcher{
		Client:   &http.Client{Transport: &stubTransport{status: 200, body: tarball}},
		CacheDir: t.TempDir(),
	}
	dir, err := f.Fetch(domain.Source{
		Name: "remote", Kind: domain.SourceGitHub,
		Location: "https://github.com/owner/repo", Branch: "main", Subpath: "skills",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// Subpath applied: the returned dir should contain deploy/SKILL.md.
	if _, err := os.Stat(filepath.Join(dir, "deploy", "SKILL.md")); err != nil {
		t.Errorf("expected skills subpath content, got: %v", err)
	}
}

func TestFetchGitHubErrorsOnBadStatus(t *testing.T) {
	f := &Fetcher{
		Client:   &http.Client{Transport: &stubTransport{status: 404, body: []byte("nope")}},
		CacheDir: t.TempDir(),
	}
	_, err := f.Fetch(domain.Source{Name: "r", Kind: domain.SourceGitHub, Location: "https://github.com/o/r"})
	if err == nil {
		t.Error("expected error on 404")
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
	tarball := makeTarGz(t, "repo-main", map[string]string{"SKILL.md": "x"})
	f := &Fetcher{
		Client:   &http.Client{Transport: &stubTransport{status: 200, body: tarball}},
		CacheDir: t.TempDir(),
	}
	src := domain.Source{Name: "remote", Kind: domain.SourceGitHub, Location: "https://github.com/owner/repo"}
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
