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

func TestSuggestionRoundTripAndQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	c := &Config{
		Targets: []TargetEntry{{Name: "claude-code", Path: "~/.claude/skills"}},
		Suggestions: []SuggestionEntry{
			{From: "review", To: "setup-matt-pocock-skills"}, // specific pair
			{From: "ask-matt"}, // bulk: all outgoing edges
		},
	}
	if err := Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %+v", got.Suggestions)
	}
	// Specific pair: only that exact edge is a Suggestion.
	if !got.IsSuggestion("review", "setup-matt-pocock-skills") {
		t.Error("review→setup-matt-pocock-skills should be a Suggestion")
	}
	if got.IsSuggestion("review", "tdd") {
		t.Error("review→tdd should NOT be a Suggestion")
	}
	// Bulk: every outgoing edge of ask-matt is a Suggestion.
	if !got.IsSuggestion("ask-matt", "tdd") || !got.IsSuggestion("ask-matt", "prototype") {
		t.Error("all ask-matt edges should be Suggestions via the bulk entry")
	}
	if !got.HasBulkSuggestion("ask-matt") || got.HasBulkSuggestion("review") {
		t.Error("HasBulkSuggestion mismatch")
	}
}

func TestAddRemoveSuggestion(t *testing.T) {
	c := &Config{}
	c.AddSuggestion("review", "setup-matt-pocock-skills")
	c.AddSuggestion("review", "setup-matt-pocock-skills") // idempotent
	if len(c.Suggestions) != 1 {
		t.Fatalf("AddSuggestion not idempotent: %+v", c.Suggestions)
	}
	if !c.IsSuggestion("review", "setup-matt-pocock-skills") {
		t.Error("edge should be a Suggestion after AddSuggestion")
	}
	c.RemoveSuggestion("review", "setup-matt-pocock-skills")
	if c.IsSuggestion("review", "setup-matt-pocock-skills") {
		t.Error("edge should be a Dependency after RemoveSuggestion")
	}
	// Adding when already covered by a bulk entry is a no-op.
	c.Suggestions = []SuggestionEntry{{From: "ask-matt"}}
	c.AddSuggestion("ask-matt", "tdd")
	if len(c.Suggestions) != 1 {
		t.Fatalf("AddSuggestion should be a no-op under a bulk entry: %+v", c.Suggestions)
	}
}

func TestSuggestionRequiresFrom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, &Config{Suggestions: []SuggestionEntry{{To: "tdd"}}}); err == nil {
		t.Fatal("expected error saving a suggestion with no from")
	}
}
