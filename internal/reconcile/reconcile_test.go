package reconcile

import (
	"testing"

	"github.com/earada/skillmux/internal/domain"
)

func ops(t *testing.T, p Plan, want int) {
	t.Helper()
	if len(p.Operations) != want {
		t.Fatalf("expected %d operations, got %d: %+v", want, len(p.Operations), p.Operations)
	}
}

func TestInstallWhenDesiredAndNotInstalled(t *testing.T) {
	p := Reconcile(
		[]Cell{{Skill: "deploy", Source: "srcA", Target: "t1"}},
		[]AvailableSkill{{Name: "deploy", Source: "srcA", Fingerprint: "h1"}},
		nil,
	)
	ops(t, p, 1)
	if p.Operations[0].Kind != Install {
		t.Errorf("kind = %q, want install", p.Operations[0].Kind)
	}
}

func TestNoOpWhenUpToDate(t *testing.T) {
	installed := []domain.Installation{{SkillName: "deploy", SourceName: "srcA", TargetName: "t1", Fingerprint: "h1"}}
	p := Reconcile(
		[]Cell{{Skill: "deploy", Source: "srcA", Target: "t1"}},
		[]AvailableSkill{{Name: "deploy", Source: "srcA", Fingerprint: "h1"}},
		installed,
	)
	ops(t, p, 0)
}

func TestReinstallWhenUpstreamDrift(t *testing.T) {
	installed := []domain.Installation{{SkillName: "deploy", SourceName: "srcA", TargetName: "t1", Fingerprint: "old"}}
	p := Reconcile(
		[]Cell{{Skill: "deploy", Source: "srcA", Target: "t1"}},
		[]AvailableSkill{{Name: "deploy", Source: "srcA", Fingerprint: "new"}},
		installed,
	)
	ops(t, p, 1)
	if p.Operations[0].Kind != Reinstall || p.Operations[0].Reason != ReasonUpdateAvailable {
		t.Errorf("got %+v, want reinstall/update-available", p.Operations[0])
	}
}

func TestUninstallWhenInstalledButNotDesired(t *testing.T) {
	installed := []domain.Installation{{SkillName: "deploy", SourceName: "srcA", TargetName: "t1", Fingerprint: "h1"}}
	p := Reconcile(nil, []AvailableSkill{{Name: "deploy", Source: "srcA", Fingerprint: "h1"}}, installed)
	ops(t, p, 1)
	if p.Operations[0].Kind != Uninstall {
		t.Errorf("kind = %q, want uninstall", p.Operations[0].Kind)
	}
}

func TestReinstallWhenSourceSwitched(t *testing.T) {
	installed := []domain.Installation{{SkillName: "deploy", SourceName: "srcA", TargetName: "t1", Fingerprint: "h1"}}
	p := Reconcile(
		[]Cell{{Skill: "deploy", Source: "srcB", Target: "t1"}},
		[]AvailableSkill{{Name: "deploy", Source: "srcB", Fingerprint: "h1"}},
		installed,
	)
	ops(t, p, 1)
	if p.Operations[0].Kind != Reinstall || p.Operations[0].Reason != ReasonSourceChanged {
		t.Errorf("got %+v, want reinstall/source-changed", p.Operations[0])
	}
}

func TestConflictWhenSameNameTwoSourcesOneTarget(t *testing.T) {
	p := Reconcile(
		[]Cell{
			{Skill: "deploy", Source: "srcA", Target: "t1"},
			{Skill: "deploy", Source: "srcB", Target: "t1"},
		},
		[]AvailableSkill{
			{Name: "deploy", Source: "srcA", Fingerprint: "h1"},
			{Name: "deploy", Source: "srcB", Fingerprint: "h2"},
		},
		nil,
	)
	// A conflict is reported, and neither install is emitted.
	var conflicts, installs int
	for _, o := range p.Operations {
		switch o.Kind {
		case Conflict:
			conflicts++
		case Install:
			installs++
		}
	}
	if conflicts != 1 || installs != 0 {
		t.Fatalf("expected 1 conflict and 0 installs, got %+v", p.Operations)
	}
}

func TestPlanIsDeterministicallyOrdered(t *testing.T) {
	desired := []Cell{
		{Skill: "b", Source: "s", Target: "t2"},
		{Skill: "a", Source: "s", Target: "t1"},
	}
	avail := []AvailableSkill{{Name: "a", Source: "s", Fingerprint: "x"}, {Name: "b", Source: "s", Fingerprint: "x"}}
	p1 := Reconcile(desired, avail, nil)
	p2 := Reconcile(desired, avail, nil)
	if len(p1.Operations) != len(p2.Operations) {
		t.Fatal("nondeterministic length")
	}
	for i := range p1.Operations {
		if p1.Operations[i] != p2.Operations[i] {
			t.Fatalf("nondeterministic order at %d: %+v vs %+v", i, p1.Operations[i], p2.Operations[i])
		}
	}
}
