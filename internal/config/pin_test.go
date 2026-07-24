package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinMatchingScopes(t *testing.T) {
	c := &Config{Pins: []PinEntry{
		{Skill: "deploy", Source: "mp"}, // exactly one row
		{Skill: "review"},               // that Skill, whatever Source
		{Source: "frozen-repo"},         // every Skill from that Source
	}}
	cases := []struct {
		skill, source string
		want          bool
	}{
		{"deploy", "mp", true},
		{"deploy", "other", false}, // exact entry does not span Sources
		{"review", "mp", true},
		{"review", "anywhere", true}, // Skill-wide
		{"anything", "frozen-repo", true},
		{"anything", "elsewhere", false},
	}
	for _, tc := range cases {
		if got := c.IsPinned(tc.skill, tc.source); got != tc.want {
			t.Errorf("IsPinned(%q, %q) = %v, want %v", tc.skill, tc.source, got, tc.want)
		}
	}
}

func TestHasWidePinOnlyForBroaderEntries(t *testing.T) {
	exact := &Config{Pins: []PinEntry{{Skill: "deploy", Source: "mp"}}}
	if exact.HasWidePin("deploy", "mp") {
		t.Error("an exact entry is not a wide pin — a per-row toggle can drop it")
	}
	for _, wide := range []PinEntry{{Skill: "deploy"}, {Source: "mp"}} {
		c := &Config{Pins: []PinEntry{wide}}
		if !c.HasWidePin("deploy", "mp") {
			t.Errorf("%+v should count as a wide pin", wide)
		}
	}
}

func TestAddAndRemovePin(t *testing.T) {
	c := &Config{}
	c.AddPin("deploy", "mp")
	c.AddPin("deploy", "mp") // idempotent
	if len(c.Pins) != 1 || !c.IsPinned("deploy", "mp") {
		t.Fatalf("pins = %+v", c.Pins)
	}
	c.RemovePin("deploy", "mp")
	if len(c.Pins) != 0 || c.IsPinned("deploy", "mp") {
		t.Fatalf("pins after remove = %+v", c.Pins)
	}

	// A wider entry already covers the row, so adding the exact pair is a no-op…
	c.Pins = []PinEntry{{Skill: "deploy"}}
	c.AddPin("deploy", "mp")
	if len(c.Pins) != 1 {
		t.Fatalf("wider pin should absorb the exact one: %+v", c.Pins)
	}
	// …and removing the exact pair must not silently narrow it.
	c.RemovePin("deploy", "mp")
	if !c.IsPinned("deploy", "mp") {
		t.Errorf("RemovePin must not narrow a Skill-wide entry: %+v", c.Pins)
	}
}

func TestPinRoundTripsThroughTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	c := &Config{
		Targets: []TargetEntry{{Name: "cc", Path: "~/.claude/skills"}},
		Sources: []SourceEntry{{Name: "mp", Location: "https://github.com/o/r"}},
		Pins:    []PinEntry{{Skill: "deploy", Source: "mp"}, {Source: "mp"}},
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[[pin]]") {
		t.Errorf("pins should be written as [[pin]] entries:\n%s", data)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pins) != 2 || got.Pins[0].Skill != "deploy" || got.Pins[1].Source != "mp" {
		t.Fatalf("pins did not round-trip: %+v", got.Pins)
	}
}

// An entry with neither field would freeze every Skill from every Source — almost
// certainly a typo, so the format rejects it rather than silently obeying.
func TestEmptyPinRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, &Config{Pins: []PinEntry{{}}}); err == nil {
		t.Fatal("an empty pin entry should be rejected")
	}
	if err := os.WriteFile(path, []byte("[[pin]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "pin") {
		t.Fatalf("loading an empty pin should error, got %v", err)
	}
}
