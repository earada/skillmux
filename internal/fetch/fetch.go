// Package fetch resolves a Source to a local directory ready to scan and hash.
// A local Source resolves to its folder directly; a GitHub Source is downloaded
// as a tarball over HTTPS (no git dependency) and extracted into the cache. The
// optional Subpath narrows the returned directory. See design Q9.
//
// Private repos are supported via an ambient credential, never stored in the
// Config: when a token is resolved (GH_TOKEN, then GITHUB_TOKEN, then
// `gh auth token`) the authenticated GitHub API tarball endpoint is used with an
// Authorization header; without a token the anonymous codeload path is used, as
// before. Only github.com is supported — GitHub Enterprise is out of scope. See
// ADR 0004.
package fetch

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/paths"
)

// Fetcher resolves Sources, caching downloaded GitHub tarballs under CacheDir.
type Fetcher struct {
	// Client is the HTTP client used for GitHub downloads; nil means
	// http.DefaultClient.
	Client *http.Client
	// CacheDir is where extracted GitHub Sources are stored.
	CacheDir string
	// Token, when non-nil, supplies the GitHub credential instead of the real
	// environment/gh resolver. Used in tests to stay hermetic. An empty string
	// means "no token" (anonymous fetch), not an error.
	Token func() (string, error)

	tokenOnce sync.Once
	tokenVal  string
	tokenErr  error
}

// Fetch returns the local directory holding the Source's Skills, with Subpath
// applied. For GitHub Sources it downloads and extracts the tarball into the
// cache (replacing any previous copy); for local Sources it resolves the path.
func (f *Fetcher) Fetch(src domain.Source) (string, error) {
	switch src.Kind {
	case domain.SourceLocal:
		return f.fetchLocal(src)
	case domain.SourceGitHub:
		return f.fetchGitHub(src)
	default:
		return "", fmt.Errorf("source %q: unknown kind %q", src.Name, src.Kind)
	}
}

func (f *Fetcher) fetchLocal(src domain.Source) (string, error) {
	base := paths.ExpandHome(src.Location)
	dir, err := applySubpath(base, src.Subpath)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("local source %q: %w", src.Name, err)
	}
	return dir, nil
}

func (f *Fetcher) fetchGitHub(src domain.Source) (string, error) {
	token, err := f.resolveToken()
	if err != nil {
		return "", fmt.Errorf("source %q: resolving credential: %w", src.Name, err)
	}

	// With a token we hit the authenticated API tarball endpoint (which serves
	// private repos and redirects to a signed codeload URL); without one we stay
	// on the anonymous codeload path to avoid the API's unauthenticated rate
	// limit. See ADR 0004.
	var tarURL string
	if token != "" {
		tarURL, err = apiTarballURL(src.Location, src.Branch)
	} else {
		tarURL, err = TarballURL(src.Location, src.Branch)
	}
	if err != nil {
		return "", fmt.Errorf("source %q: %w", src.Name, err)
	}

	req, err := http.NewRequest(http.MethodGet, tarURL, nil)
	if err != nil {
		return "", fmt.Errorf("source %q: %w", src.Name, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading %q: %w", src.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", githubStatusError(src.Name, resp.StatusCode, token != "")
	}

	dest := filepath.Join(f.CacheDir, "github", sanitize(src.Name))
	if err := os.RemoveAll(dest); err != nil {
		return "", fmt.Errorf("clearing cache for %q: %w", src.Name, err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	if err := extractTarGz(resp.Body, dest); err != nil {
		return "", fmt.Errorf("extracting %q: %w", src.Name, err)
	}
	return applySubpath(dest, src.Subpath)
}

// CacheDirFor returns the cache directory a Source's contents are extracted
// into, or "" for Sources that are not cached (local Sources resolve in place).
func (f *Fetcher) CacheDirFor(src domain.Source) string {
	if src.Kind != domain.SourceGitHub {
		return ""
	}
	return filepath.Join(f.CacheDir, "github", sanitize(src.Name))
}

// ClearCache removes a Source's cached copy from disk. It reports whether the
// Source is cacheable: false (with no error) for local Sources, which have no
// cache. The next Fetch re-downloads the Source.
func (f *Fetcher) ClearCache(src domain.Source) (bool, error) {
	dir := f.CacheDirFor(src)
	if dir == "" {
		return false, nil
	}
	return true, os.RemoveAll(dir)
}

// TarballURL builds the codeload tarball URL for a GitHub repo URL and ref.
// branch may be a branch, tag, or commit; empty means the default branch (HEAD).
func TarballURL(repoURL, branch string) (string, error) {
	owner, repo, err := ownerRepo(repoURL)
	if err != nil {
		return "", err
	}
	ref := branch
	if ref == "" {
		ref = "HEAD"
	}
	return fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, ref), nil
}

// apiTarballURL builds the authenticated GitHub API tarball endpoint for a repo
// URL and ref. An empty branch omits the ref, which the API resolves to the
// repository's default branch. Used for private (token-bearing) fetches.
func apiTarballURL(repoURL, branch string) (string, error) {
	owner, repo, err := ownerRepo(repoURL)
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/tarball", owner, repo)
	if branch != "" {
		u += "/" + branch
	}
	return u, nil
}

// resolveToken returns the ambient GitHub credential, evaluated once and memoized
// for the Fetcher's lifetime. An empty string means "no token" (anonymous fetch).
func (f *Fetcher) resolveToken() (string, error) {
	f.tokenOnce.Do(func() {
		if f.Token != nil {
			f.tokenVal, f.tokenErr = f.Token()
			return
		}
		f.tokenVal, f.tokenErr = defaultToken()
	})
	return f.tokenVal, f.tokenErr
}

// defaultToken resolves the GitHub credential from the environment, falling back
// to the gh CLI: GH_TOKEN, then GITHUB_TOKEN, then `gh auth token`. A missing or
// unauthenticated gh is not an error — it yields an empty token so the fetch
// proceeds anonymously and any real failure surfaces at download time.
func defaultToken() (string, error) {
	if t := strings.TrimSpace(os.Getenv("GH_TOKEN")); t != "" {
		return t, nil
	}
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t, nil
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return "", nil
	}
	out, err := exec.Command(gh, "auth", "token").Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// githubStatusError maps a non-200 GitHub response to an actionable error. It
// never includes the token. A 404 is ambiguous (missing vs. private-without-
// access), so the hint depends on whether a token was sent.
func githubStatusError(name string, status int, hadToken bool) error {
	switch status {
	case http.StatusNotFound:
		if hadToken {
			return fmt.Errorf("source %q: repository not found, or your token has no access to it (404)", name)
		}
		return fmt.Errorf("source %q: repository not found or private — set GH_TOKEN/GITHUB_TOKEN or run `gh auth login` (404)", name)
	case http.StatusUnauthorized:
		return fmt.Errorf("source %q: authentication failed — token invalid or expired (401)", name)
	case http.StatusForbidden:
		return fmt.Errorf("source %q: access forbidden — token lacks the required scope, or rate limit exceeded (403)", name)
	default:
		return fmt.Errorf("downloading %q: unexpected status %d", name, status)
	}
}

func ownerRepo(repoURL string) (string, string, error) {
	var path string
	if strings.HasPrefix(repoURL, "git@") {
		// git@github.com:owner/repo(.git)
		_, after, ok := strings.Cut(repoURL, ":")
		if !ok {
			return "", "", fmt.Errorf("invalid ssh URL %q", repoURL)
		}
		path = after
	} else {
		u, err := url.Parse(repoURL)
		if err != nil {
			return "", "", fmt.Errorf("invalid URL %q: %w", repoURL, err)
		}
		path = u.Path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("URL %q does not contain owner/repo", repoURL)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// extractTarGz extracts a gzipped tar into dest, stripping the single top-level
// directory that GitHub archives wrap their contents in.
func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := stripTopDir(hdr.Name)
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, rel)
		if !within(dest, target) {
			return fmt.Errorf("tar entry %q escapes destination", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil { //nolint:gosec // size bounded by archive
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
		// Other entry types (symlinks, devices) are skipped.
	}
}

// stripTopDir removes the first path component (the archive's wrapper dir).
func stripTopDir(name string) string {
	name = filepath.ToSlash(name)
	idx := strings.IndexByte(name, '/')
	if idx < 0 {
		return "" // the top dir entry itself
	}
	return name[idx+1:]
}

// within reports whether target is inside base after cleaning, guarding against
// path traversal in archive entries.
func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// applySubpath joins an optional, traversal-free subpath onto base.
func applySubpath(base, subpath string) (string, error) {
	if subpath == "" {
		return base, nil
	}
	clean := filepath.Clean("/" + filepath.ToSlash(subpath)) // anchor to strip ".."
	return filepath.Join(base, clean), nil
}

func sanitize(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, name)
}
