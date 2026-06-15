// Package fetch resolves a Source to a local directory ready to scan and hash.
// A local Source resolves to its folder directly; a GitHub Source is downloaded
// as a tarball over HTTPS (no git dependency, public repos only in v1) and
// extracted into the cache. The optional Subpath narrows the returned directory.
// See design Q9.
package fetch

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/earada/skillmux/internal/domain"
)

// Fetcher resolves Sources, caching downloaded GitHub tarballs under CacheDir.
type Fetcher struct {
	// Client is the HTTP client used for GitHub downloads; nil means
	// http.DefaultClient.
	Client *http.Client
	// CacheDir is where extracted GitHub Sources are stored.
	CacheDir string
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
	base := expandHome(src.Location)
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
	tarURL, err := TarballURL(src.Location, src.Branch)
	if err != nil {
		return "", fmt.Errorf("source %q: %w", src.Name, err)
	}

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(tarURL)
	if err != nil {
		return "", fmt.Errorf("downloading %q: %w", src.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %q: unexpected status %d", src.Name, resp.StatusCode)
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

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
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
