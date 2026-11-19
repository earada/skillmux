package fetch

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/earada/skillmux/internal/domain"
)

// unsupportedMarker is the phrase every Location-validation error carries.
const unsupportedMarker = "only github.com over https or ssh is supported"

// TestFetchGitHubLocationValidation drives Locations through Fetcher.Fetch with
// a deadline so short that any git process that does start times out at once.
// A rejected Location must fail with the validation error (no git ever runs);
// an accepted one must get past validation and reach git (here: the timeout).
func TestFetchGitHubLocationValidation(t *testing.T) {
	requireGit(t)
	local := filepath.Join(t.TempDir(), "owner", "repo")

	accepted := []string{
		"https://github.com/owner/repo",
		"https://github.com/owner/repo.git",
		"https://github.com/owner/repo/",
		"https://GitHub.COM/owner/repo",
		"ssh://git@github.com/owner/repo.git",
		"ssh://git@GITHUB.com/owner/repo",
		"git@github.com:owner/repo.git",
		"git@GitHub.com:owner/repo",
		// Test/mirror seam: filesystem paths and file:// URLs stay accepted.
		local,
		"file://" + local,
	}
	rejected := []struct{ loc, wantExtra string }{
		{"http://github.com/owner/repo", "use https://"},
		{"HTTP://github.com/owner/repo", "use https://"},
		{"git://github.com/owner/repo", ""},
		{"ftp://github.com/owner/repo", ""},
		{"https://evil.example/owner/repo", ""},
		{"https://github.com.evil.example/owner/repo", ""},
		{"https://github.com:8443/owner/repo", ""},
		{"https://github.com@evil.example/owner/repo", ""},
		{"ssh://git@evil.example/owner/repo", ""},
		{"ssh://evil@github.com/owner/repo", ""},
		{"ssh://github.com/owner/repo", ""},
		{"git@other.host:owner/repo", ""},
		{"git@github.com.evil.example:owner/repo", ""},
		// scp-style / transport::address forms git would treat as remote.
		{"evil.example:owner/repo", ""},
		{"user@evil.example:owner/repo", ""},
		{"ext::sh -c touch% /tmp/pwned", ""},
	}

	f := &Fetcher{CacheDir: t.TempDir(), Timeout: time.Nanosecond}
	for _, loc := range accepted {
		t.Run("accept "+loc, func(t *testing.T) {
			_, err := f.Fetch(domain.Source{Name: "s", Kind: domain.SourceGitHub, Location: loc})
			if err != nil && strings.Contains(err.Error(), unsupportedMarker) {
				t.Fatalf("Location %q rejected by validation: %v", loc, err)
			}
		})
	}
	for _, c := range rejected {
		t.Run("reject "+c.loc, func(t *testing.T) {
			_, err := f.Fetch(domain.Source{Name: "s", Kind: domain.SourceGitHub, Location: c.loc})
			if err == nil {
				t.Fatalf("Location %q accepted; want validation error", c.loc)
			}
			msg := err.Error()
			if !strings.Contains(msg, unsupportedMarker) {
				t.Fatalf("Location %q: got %v; want validation error containing %q", c.loc, err, unsupportedMarker)
			}
			if !strings.Contains(msg, c.loc) {
				t.Errorf("Location %q: error %q does not name the Location", c.loc, msg)
			}
			if strings.Contains(msg, "timed out") || strings.Contains(msg, "git ") {
				t.Errorf("Location %q: error %q suggests git ran", c.loc, msg)
			}
			if c.wantExtra != "" && !strings.Contains(msg, c.wantExtra) {
				t.Errorf("Location %q: error %q missing hint %q", c.loc, msg, c.wantExtra)
			}
		})
	}
}
