package engine

import (
	"strings"
	"testing"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/manifest"
)

func TestRenameSourceKeepsInstallationsAndPins(t *testing.T) {
	e, _, _, manifestPath := newEnv(t)
	cat := e.Refresh()
	if rep, err := e.Apply(e.Preview(cell(), cat), apply.Options{}); err != nil || !rep.AllOK() {
		t.Fatalf("install failed: %v %+v", err, rep)
	}
	e.Config.Pins = []config.PinEntry{{Skill: "deploy", Source: "local"}, {Source: "local"}, {Skill: "other"}}
	s := e.Config.Sources[0]
	s.Name = "crew"
	if err := e.UpdateSource("local", s); err != nil {
		t.Fatal(err)
	}

	cat = e.Refresh()
	cells := e.Status(cat)
	if len(cells) != 1 || cells[0].SourceName != "crew" || cells[0].Status != domain.StatusPinned {
		t.Fatalf("want one pinned cell from the renamed Source, got %+v", cells)
	}
	if un := e.UnavailableSkills(cat); len(un) != 0 {
		t.Fatalf("renamed Source left unavailable rows: %+v", un)
	}
	want := []config.PinEntry{{Skill: "deploy", Source: "crew"}, {Source: "crew"}, {Skill: "other"}}
	for i, p := range e.Config.Pins {
		if p != want[i] {
			t.Fatalf("pins = %+v, want %+v", e.Config.Pins, want)
		}
	}
	man, err := manifest.Load(manifestPath)
	if err != nil || len(man.Installations) != 1 || man.Installations[0].SourceName != "crew" {
		t.Fatalf("persisted manifest not re-keyed: %+v %v", man, err)
	}
	loaded, err := config.Load(e.configPath)
	if err != nil || len(loaded.Pins) != 3 || loaded.Pins[0].Source != "crew" {
		t.Fatalf("persisted pins not re-keyed: %+v %v", loaded, err)
	}
}

func TestFailedSourceRenameRollsBackPins(t *testing.T) {
	e, _, _, _ := newEnv(t)
	e.Config.Sources = append(e.Config.Sources, config.SourceEntry{Name: "taken", Location: t.TempDir()})
	e.Config.Pins = []config.PinEntry{{Source: "local"}}
	s := e.Config.Sources[0]
	s.Name = "taken" // duplicate: the save must fail
	if err := e.UpdateSource("local", s); err == nil {
		t.Fatal("expected a duplicate-name error")
	}
	if e.Config.Pins[0].Source != "local" || e.Config.Sources[0].Name != "local" {
		t.Fatalf("failed rename leaked into Config: %+v", e.Config)
	}
}

func TestSourceFormRejectsWhatFetchWouldRefuse(t *testing.T) {
	e, _, _, _ := newEnv(t)
	for _, s := range []config.SourceEntry{
		{Name: "a", Location: "http://github.com/o/r"},
		{Name: "b", Location: "https://evil.example/o/r"},
		{Name: "c", Location: "https://github.com/o/r", Branch: "--upload-pack=x"},
	} {
		if err := e.AddSource(s); err == nil {
			t.Errorf("AddSource(%+v) accepted", s)
		}
		if err := e.UpdateSource("local", s); err == nil {
			t.Errorf("UpdateSource(%+v) accepted", s)
		}
	}
	if len(e.Config.Sources) != 1 || e.Config.Sources[0].Name != "local" {
		t.Fatalf("rejected Source reached Config: %+v", e.Config.Sources)
	}
	if err := e.AddSource(config.SourceEntry{Name: "ok", Location: "https://github.com/o/r", Branch: "main"}); err != nil {
		t.Fatalf("valid Source rejected: %v", err)
	}
	// A bad Source already in a hand-edited Config must not block other saves.
	e.Config.Sources = append(e.Config.Sources, config.SourceEntry{Name: "legacy", Location: "http://github.com/o/r"})
	if err := e.AddTarget("extra", t.TempDir()); err != nil || !strings.Contains(e.Config.Targets[1].Name, "extra") {
		t.Fatalf("unrelated save blocked: %v", err)
	}
}
