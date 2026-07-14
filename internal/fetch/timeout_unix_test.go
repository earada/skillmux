//go:build unix

package fetch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/earada/skillmux/internal/domain"
)

// fakeBlockingGit installs a `git` on PATH (ahead of the real one) that records
// its pid and then blocks effectively forever, standing in for a git that hangs
// on an SSH prompt, a dead credential helper, or a stalled network read. It
// returns the path of the file the fake writes its pid to.
func fakeBlockingGit(t *testing.T) (pidFile string) {
	t.Helper()
	bin := t.TempDir()
	pidFile = filepath.Join(t.TempDir(), "git.pid")
	// `exec sleep` replaces the shell so the recorded pid is the process the
	// timeout must actually kill; a very long sleep guarantees the test's own
	// short timeout is what ends it.
	script := "#!/bin/sh\necho $$ > \"" + pidFile + "\"\nexec sleep 300\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return pidFile
}

func alive(pid int) bool {
	// Signal 0 probes for the process without affecting it.
	return syscall.Kill(pid, 0) == nil
}

// TestFetchTimesOutOnBlockingGit is the acceptance test: a git that never
// returns must not pin Refresh. Fetch has to come back promptly with a timeout
// error that names the Source, and the blocked process must be gone.
func TestFetchTimesOutOnBlockingGit(t *testing.T) {
	pidFile := fakeBlockingGit(t)
	f := &Fetcher{CacheDir: t.TempDir(), Timeout: time.Second}
	src := domain.Source{Name: "hangs", Kind: domain.SourceGitHub, Location: "https://github.com/o/r"}

	start := time.Now()
	_, err := f.Fetch(src)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error from a git that never returns")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should report a timeout, got: %v", err)
	}
	if !strings.Contains(err.Error(), "hangs") {
		t.Errorf("error should name the Source, got: %v", err)
	}
	// Promptly: comfortably under the fake git's 300s sleep, and near the 200ms
	// deadline rather than anywhere close to hanging.
	if elapsed > 10*time.Second {
		t.Errorf("Fetch took %s; expected it to fail fast on the timeout", elapsed)
	}

	// No leaked process: the timeout must have torn down the blocked git.
	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("fake git did not record its pid: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Fatalf("bad pid file %q: %v", data, convErr)
	}
	// Give the kill a beat to be reflected, then assert the process is gone.
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		// Clean up the leak so it does not outlive the test run.
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("blocked git (pid %d) leaked past the timeout", pid)
	}
}

// TestNonInteractiveGitEnv verifies credential prompts are forced off:
// GIT_TERMINAL_PROMPT is pinned to 0 and ssh runs in BatchMode.
func TestNonInteractiveGitEnv(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /custom/key")

	env := nonInteractiveGitEnv()
	term := lastValue(env, "GIT_TERMINAL_PROMPT=")
	if term != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT = %q, want 0", term)
	}
	ssh := lastValue(env, "GIT_SSH_COMMAND=")
	if !strings.Contains(ssh, "BatchMode=yes") {
		t.Errorf("GIT_SSH_COMMAND %q should force BatchMode=yes", ssh)
	}
	// A user's own ssh options must be preserved, not discarded.
	if !strings.Contains(ssh, "/custom/key") {
		t.Errorf("GIT_SSH_COMMAND %q should preserve the user's ssh command", ssh)
	}
	// GIT_TERMINAL_PROMPT must appear exactly once (the inherited one dropped).
	if n := countPrefix(env, "GIT_TERMINAL_PROMPT="); n != 1 {
		t.Errorf("GIT_TERMINAL_PROMPT appears %d times, want 1", n)
	}
}

func lastValue(env []string, prefix string) string {
	v := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			v = strings.TrimPrefix(kv, prefix)
		}
	}
	return v
}

func countPrefix(env []string, prefix string) int {
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			n++
		}
	}
	return n
}
