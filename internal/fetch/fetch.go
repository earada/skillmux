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
//
// Every git invocation is bounded by a timeout (defaultGitTimeout, overridable
// via Fetcher.Timeout) and runs with all interactive credential prompts
// disabled, so a stalled clone/fetch — an SSH host-key or passphrase prompt, a
// dead credential helper, or a hung network/DNS lookup — cannot pin the
// background Refresh in a permanently refreshing state; it returns a timeout
// error naming the Source and action instead.
package fetch

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/paths"
)

// defaultGitTimeout bounds every individual git invocation. It is the fail-fast
// deadline that keeps a background Refresh from hanging forever when a git
// operation stalls (SSH host-key / passphrase prompt, an unreachable host, a
// dead credential helper, or a hung DNS lookup). Generous enough for a slow
// clone over a poor connection, short enough that the TUI recovers on its own.
const defaultGitTimeout = 2 * time.Minute

// Fetcher resolves Sources, caching GitHub clones under CacheDir.
type Fetcher struct {
	// CacheDir is where GitHub Sources are cloned.
	CacheDir string
	// Timeout bounds each git invocation; a zero value uses defaultGitTimeout.
	// Overridable mainly so tests can pin a short deadline.
	Timeout time.Duration
}

// gitTimeout is the per-invocation git deadline, defaulting when unset.
func (f *Fetcher) gitTimeout() time.Duration {
	if f.Timeout > 0 {
		return f.Timeout
	}
	return defaultGitTimeout
}

// Fetch returns the local directory holding the Source's Skills, with Subpath
// applied. For GitHub Sources it clones (or updates: fetch + checkout) the
// repository under the cache; for local Sources it resolves the path.
func (f *Fetcher) Fetch(src domain.Source) (string, error) {
	return f.resolve(src, false)
}

// FetchObjectsOnly updates a GitHub Source's git objects without touching its
// working tree (git fetch, no reset) — used to refresh a Source whose files are
// being viewed, deferring the checkout until the view closes so the file
// explorer never reads a half-rewritten tree. A Source with no existing clone is
// cloned normally (there is no working tree to protect yet); a local Source
// behaves exactly like Fetch.
func (f *Fetcher) FetchObjectsOnly(src domain.Source) (string, error) {
	return f.resolve(src, true)
}

func (f *Fetcher) resolve(src domain.Source, skipCheckout bool) (string, error) {
	switch src.Kind {
	case domain.SourceLocal:
		return f.fetchLocal(src)
	case domain.SourceGitHub:
		return f.fetchGitHub(src, skipCheckout)
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
func (f *Fetcher) fetchGitHub(src domain.Source, skipCheckout bool) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("source %q: skillmux requires git on PATH to fetch GitHub Sources — install git, or use a local Source", src.Name)
	}
	// Validate the Location names a repository (owner/repo), but clone the
	// original string so an SSH Location is preserved verbatim.
	if _, _, err := ownerRepo(src.Location); err != nil {
		return "", fmt.Errorf("source %q: %w", src.Name, err)
	}

	timeout := f.gitTimeout()
	dest := f.CacheDirFor(src)
	if isGitRepo(dest) {
		// The cache dir is keyed by Source name, so an existing clone may be
		// left over from a previous Location under the same name. Point origin
		// at the current Location (a no-op when unchanged) before updating, so a
		// Refresh after a Location edit fetches the new repository rather than
		// silently serving stale content from the old one.
		if err := syncOrigin(timeout, dest, src.Location); err != nil {
			return "", fmt.Errorf("source %q: %w", src.Name, err)
		}
		if err := updateClone(timeout, dest, src.Branch, skipCheckout); err != nil {
			return "", fmt.Errorf("source %q: %w", src.Name, err)
		}
	} else {
		if err := freshClone(timeout, src.Location, dest, src.Branch); err != nil {
			return "", fmt.Errorf("source %q: %w", src.Name, err)
		}
	}
	return applySubpath(dest, src.Subpath)
}

// syncOrigin makes the clone's origin remote point at repoURL, updating it when
// the Source Location has changed since the clone was created. The stored URL is
// compared verbatim against the value git recorded at clone time, so an
// unchanged Location leaves the remote untouched.
func syncOrigin(timeout time.Duration, dest, repoURL string) error {
	current, err := runGit(timeout, dest, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if strings.TrimSpace(current) == repoURL {
		return nil
	}
	_, err = runGit(timeout, dest, "remote", "set-url", "origin", repoURL)
	return err
}

// freshClone removes any stale contents at dest and shallow-clones the ref into
// it. An empty ref clones the repository's default branch. A commit SHA cannot
// be cloned with --branch, so it is fetched explicitly into a freshly
// initialised repo instead (GitHub serves a reachable SHA over a shallow fetch).
func freshClone(timeout time.Duration, repoURL, dest, ref string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if looksLikeCommitSHA(ref) {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		if _, err := runGit(timeout, dest, "init", "-q"); err != nil {
			return err
		}
		if _, err := runGit(timeout, dest, "remote", "add", "origin", repoURL); err != nil {
			return err
		}
		// No working tree to protect on a fresh clone, so always check out.
		return updateClone(timeout, dest, ref, false)
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref, "--single-branch")
	}
	args = append(args, "--", repoURL, dest)
	_, err := runGit(timeout, "", args...)
	return err
}

// looksLikeCommitSHA reports whether ref is a lowercase hex commit id (7–40
// chars) rather than a branch or tag name — such a ref must be fetched
// explicitly, not passed to `git clone --branch`. A branch literally named like
// a hex string is vanishingly rare and not worth distinguishing.
func looksLikeCommitSHA(ref string) bool {
	if len(ref) < 7 || len(ref) > 40 {
		return false
	}
	for _, r := range ref {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// updateClone brings an existing clone up to the requested ref by fetching it
// shallowly and (unless skipCheckout) hard-resetting the working tree to it. An
// empty ref tracks the remote's default branch (HEAD). Fetching an explicit ref
// works regardless of the clone's configured refspec, so this also handles a
// changed Branch. With skipCheckout the objects are updated but the working tree
// is left untouched, so a later checkout can apply the update when it is safe.
func updateClone(timeout time.Duration, dest, ref string, skipCheckout bool) error {
	fetchRef := ref
	if fetchRef == "" {
		fetchRef = "HEAD"
	}
	if _, err := runGit(timeout, dest, "fetch", "--depth", "1", "origin", fetchRef); err != nil {
		return err
	}
	if skipCheckout {
		return nil
	}
	if _, err := runGit(timeout, dest, "reset", "--hard", "FETCH_HEAD"); err != nil {
		return err
	}
	// Drop files that vanished upstream or were left untracked.
	if _, err := runGit(timeout, dest, "clean", "-ffd"); err != nil {
		return err
	}
	return nil
}

// runGit runs a git command (in workdir, or the process cwd when empty) under a
// timeout and returns its stdout. The command runs in its own process group so
// the deadline tears down git and anything it spawned (notably ssh) rather than
// leaking a stalled process. Every interactive credential path is disabled so a
// private repo without usable credentials — or an SSH host-key/passphrase
// prompt — fails fast instead of hanging the TUI; git's non-interactive auth
// (credential helper, ready SSH keys) is left untouched. A deadline hit yields a
// timeout error naming the git action; callers wrap it with the Source name.
func runGit(timeout time.Duration, workdir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	cmd.Env = nonInteractiveGitEnv()
	startNewProcessGroup(cmd)

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("git %s: timed out after %s", strings.Join(args, " "), timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// nonInteractiveGitEnv is the process environment with every interactive
// credential prompt forced off: GIT_TERMINAL_PROMPT=0 suppresses git's own
// username/password and host-key prompts, and ssh runs with BatchMode=yes so it
// fails fast instead of blocking on a host-key confirmation or key passphrase. A
// user's existing GIT_SSH_COMMAND is preserved (BatchMode is appended to it) so
// custom SSH configuration still applies; any inherited GIT_TERMINAL_PROMPT is
// dropped in favour of our value.
func nonInteractiveGitEnv() []string {
	base := os.Environ()
	sshCmd := "ssh"
	env := make([]string, 0, len(base)+2)
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT="):
			// Dropped; replaced with our canonical value below.
		case strings.HasPrefix(kv, "GIT_SSH_COMMAND="):
			if v := strings.TrimPrefix(kv, "GIT_SSH_COMMAND="); strings.TrimSpace(v) != "" {
				sshCmd = v
			}
		default:
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND="+sshCmd+" -oBatchMode=yes",
	)
}

// isGitRepo reports whether dir already holds a git clone.
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Revision reports the commit the Source's clone currently sits at. ok is false
// (with no error) for Sources that have no clone to inspect — local Sources, or
// a GitHub Source not yet fetched. FetchedAt is left zero for the caller to
// stamp. The ref is the configured Branch, falling back to the checked-out
// branch name when none was pinned.
func (f *Fetcher) Revision(src domain.Source) (domain.Revision, bool) {
	if src.Kind != domain.SourceGitHub {
		return domain.Revision{}, false
	}
	dir := f.CacheDirFor(src)
	if !isGitRepo(dir) {
		return domain.Revision{}, false
	}
	timeout := f.gitTimeout()
	sha, err := runGit(timeout, dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return domain.Revision{}, false
	}
	ref := src.Branch
	switch {
	case looksLikeCommitSHA(ref):
		// A SHA pin: the short SHA alone is the clearest label (avoid the
		// redundant "fullsha @ shortsha").
		ref = ""
	case ref == "":
		// Default branch: resolve the checked-out branch name, but leave it
		// empty when HEAD is detached so the label is just the short SHA.
		if out, err := runGit(timeout, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			if b := strings.TrimSpace(out); b != "HEAD" {
				ref = b
			}
		}
	}
	return domain.Revision{Ref: ref, ShortSHA: strings.TrimSpace(sha)}, true
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
