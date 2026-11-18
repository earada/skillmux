//go:build unix

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/manifest"
)

// TestRefreshFetchesSourcesConcurrently pins skillmux-b02: a Refresh over many
// GitHub Sources must not pay each Source's network latency in series. A fake
// `git` stands in for a slow remote (every invocation sleeps, then fails); with
// N Sources a sequential Refresh takes at least N×delay, a concurrent one about
// one delay.
func TestRefreshFetchesSourcesConcurrently(t *testing.T) {
	const (
		n     = 6
		delay = 400 * time.Millisecond
	)
	bin := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nsleep %.1f\necho 'fake remote unavailable' >&2\nexit 1\n", delay.Seconds())
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := &config.Config{Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}}}
	for i := 0; i < n; i++ {
		cfg.Sources = append(cfg.Sources, config.SourceEntry{
			Name:     fmt.Sprintf("gh%d", i),
			Location: fmt.Sprintf("https://github.com/owner/repo%d", i),
		})
	}
	e := New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))

	start := time.Now()
	cat := e.Refresh()
	elapsed := time.Since(start)

	if len(cat.SourceErrors) != n {
		t.Fatalf("expected %d source errors from the fake git, got %v", n, cat.SourceErrors)
	}
	if limit := time.Duration(n/2) * delay; elapsed >= limit {
		t.Errorf("Refresh of %d slow Sources took %s; want < %s (Sources fetched in series?)", n, elapsed, limit)
	}
}

// TestRefreshKeepsSourceOrder checks a concurrent Refresh still emits Skills in
// configured Source order, so the matrix rows do not shuffle between refreshes.
func TestRefreshKeepsSourceOrder(t *testing.T) {
	cfg := &config.Config{Targets: []config.TargetEntry{{Name: "cc", Path: t.TempDir()}}}
	var want []string
	for i := 0; i < 8; i++ {
		root := t.TempDir()
		name := fmt.Sprintf("skill%d", i)
		writeSkill(t, filepath.Join(root, name), name, "d", "")
		cfg.Sources = append(cfg.Sources, config.SourceEntry{Name: fmt.Sprintf("src%d", i), Location: root})
		want = append(want, name)
	}
	e := New(cfg, &manifest.Manifest{}, &fetch.Fetcher{CacheDir: t.TempDir()}, "", filepath.Join(t.TempDir(), "m.json"))

	for round := 0; round < 5; round++ {
		cat := e.Refresh()
		if len(cat.Skills) != len(want) {
			t.Fatalf("round %d: got %d skills, want %d (%v)", round, len(cat.Skills), len(want), cat.SourceErrors)
		}
		for i, sk := range cat.Skills {
			if sk.Name != want[i] {
				t.Fatalf("round %d: skill %d = %q, want %q", round, i, sk.Name, want[i])
			}
		}
	}
}
