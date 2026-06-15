package config

import (
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/domain"
)

func TestLoadMissingFileYieldsEmptyConfig(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err != nil {
		t.Fatalf("Load of missing file: %v", err)
	}
	if len(c.Targets) != 0 || len(c.Sources) != 0 {
		t.Fatalf("expected empty config, got %+v", c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	want := &Config{
		Targets: []TargetEntry{{Name: "claude-code", Path: "~/.claude/skills"}},
		Sources: []SourceEntry{
			{Name: "remote", Location: "https://github.com/foo/bar", Branch: "main", Subpath: "skills"},
			{Name: "local", Location: "/home/me/skills"},
		},
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Targets) != 1 || got.Targets[0] != want.Targets[0] {
		t.Errorf("targets round-trip mismatch: %+v", got.Targets)
	}
	if len(got.Sources) != 2 || got.Sources[0] != want.Sources[0] {
		t.Errorf("sources round-trip mismatch: %+v", got.Sources)
	}
}

func TestKindInference(t *testing.T) {
	c := &Config{Sources: []SourceEntry{
		{Name: "url", Location: "https://github.com/foo/bar"},
		{Name: "ssh", Location: "git@github.com:foo/bar.git"},
		{Name: "path", Location: "/home/me/skills"},
		{Name: "forced", Location: "/looks/local", Kind: "github"},
	}}
	got := c.DomainSources()
	want := []domain.SourceKind{domain.SourceGitHub, domain.SourceGitHub, domain.SourceLocal, domain.SourceGitHub}
	for i, w := range want {
		if got[i].Kind != w {
			t.Errorf("source %q: got kind %q, want %q", got[i].Name, got[i].Kind, w)
		}
	}
}

func TestValidateRejectsDuplicatesAndEmpties(t *testing.T) {
	cases := map[string]*Config{
		"dup target":   {Targets: []TargetEntry{{Name: "a", Path: "/x"}, {Name: "a", Path: "/y"}}},
		"dup source":   {Sources: []SourceEntry{{Name: "s", Location: "/x"}, {Name: "s", Location: "/y"}}},
		"empty target": {Targets: []TargetEntry{{Name: "a"}}},
		"empty source": {Sources: []SourceEntry{{Location: "/x"}}},
	}
	for name, c := range cases {
		if err := c.validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}
