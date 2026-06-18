// Package fetch resolves a Source to a local directory ready to scan and hash.
// A local Source resolves to its folder directly; a GitHub Source is kept as a
// shallow, single-branch git clone under CacheDir and updated in place. The
// optional Subpath narrows the returned directory.
//
// git is a hard requirement for GitHub Sources: when it is not on PATH (or a
// clone/fetch fails) the fetch fails fast with an actionable error — there is
// no tarball fallback. Authentication is deferred entirely to git: private
// repos work through the user's own credential helper or SSH keys, and an
// `git@github.com:owner/repo` SSH Location clones directly. Skillmux never
// reads or stores a token. See ADR 0006 (which supersedes ADR 0004).
package fetch

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/paths"
)

// Fetcher resolves Sources, caching GitHub clones under CacheDir.
type Fetcher struct {
	// CacheDir is where GitHub Sources are cloned.
	CacheDir string
}

// Fetch returns the local directory holding the Source's Skills, with Subpath
// applied. For GitHub Sources it clones (or updates) the repository under the
// cache; for local Sources it resolves the path.
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

// fetchGitHub clones the Source on first use and updates it (fetch + reset) on
// every subsequent Fetch, leaving a shallow checkout of the requested ref under
// the cache. The returned directory is that checkout with Subpath applied.
func (f *Fetcher) fetchGitHub(src domain.Source) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("source %q: skillmux requires git on PATH to fetch GitHub Sources — install git, or use a local Source", src.Name)
	}
	// Validate the Location names a repository (owner/repo), but clone the
	// original string so an SSH Location is preserved verbatim.
	if _, _, err := ownerRepo(src.Location); err != nil {
		return "", fmt.Errorf("source %q: %w", src.Name, err)
	}

	dest := f.CacheDirFor(src)
	if isGitRepo(dest) {
		if err := updateClone(dest, src.Branch); err != nil {
			return "", fmt.Errorf("source %q: %w", src.Name, err)
		}
	} else {
		if err := freshClone(src.Location, dest, src.Branch); err != nil {
			return "", fmt.Errorf("source %q: %w", src.Name, err)
		}
	}
	return applySubpath(dest, src.Subpath)
}

// freshClone removes any stale contents at dest and shallow-clones the ref into
// it. An empty ref clones the repository's default branch.
func freshClone(repoURL, dest, ref string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref, "--single-branch")
	}
	args = append(args, "--", repoURL, dest)
	_, err := runGit("", args...)
	return err
}

// updateClone brings an existing clone up to the requested ref by fetching it
// shallowly and hard-resetting the working tree to it. An empty ref tracks the
// remote's default branch (HEAD). Fetching an explicit ref works regardless of
// the clone's configured refspec, so this also handles a changed Branch.
func updateClone(dest, ref string) error {
	fetchRef := ref
	if fetchRef == "" {
		fetchRef = "HEAD"
	}
	if _, err := runGit(dest, "fetch", "--depth", "1", "origin", fetchRef); err != nil {
		return err
	}
	if _, err := runGit(dest, "reset", "--hard", "FETCH_HEAD"); err != nil {
		return err
	}
	// Drop files that vanished upstream or were left untracked.
	if _, err := runGit(dest, "clean", "-ffd"); err != nil {
		return err
	}
	return nil
}

// runGit runs a git command (in workdir, or the process cwd when empty) and
// returns its stdout. Interactive prompts are disabled so a private repo
// without usable credentials fails fast instead of hanging the TUI. git's auth
// (credential helper, SSH) is left untouched.
func runGit(workdir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// isGitRepo reports whether dir already holds a git clone.
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// CacheDirFor returns the cache directory a Source is cloned into, or "" for
// Sources that are not cached (local Sources resolve in place).
func (f *Fetcher) CacheDirFor(src domain.Source) string {
	if src.Kind != domain.SourceGitHub {
		return ""
	}
	return filepath.Join(f.CacheDir, "github", sanitize(src.Name))
}

// ClearCache removes a Source's cached clone from disk. It reports whether the
// Source is cacheable: false (with no error) for local Sources, which have no
// cache. The next Fetch re-clones the Source.
func (f *Fetcher) ClearCache(src domain.Source) (bool, error) {
	dir := f.CacheDirFor(src)
	if dir == "" {
		return false, nil
	}
	return true, os.RemoveAll(dir)
}

// ownerRepo extracts owner and repo from a GitHub HTTPS or SSH URL. It both
// validates a Source Location and (in future) builds API URLs; here it is used
// only to reject Locations that do not name a repository.
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
