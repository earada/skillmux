package reconcile

import (
	"testing"

	"github.com/earada/skillmux/internal/domain"
)

// A Pin freezes the row against upstream drift: the catch-up Reinstall a
// fingerprint mismatch would normally produce must not be planned.
func TestPinnedSkillSkipsDriftReinstall(t *testing.T) {
	installed := []domain.Installation{{SkillName: "deploy", SourceName: "srcA", TargetName: "t1", Fingerprint: "old"}}
	p := Reconcile(
		[]Cell{{Skill: "deploy", Source: "srcA", Target: "t1"}},
		[]AvailableSkill{{Name: "deploy", Source: "srcA", Fingerprint: "new", Pinned: true}},
		installed,
		nil,
	)
	ops(t, p, 0)
}

// A Pin holds a version; it does not veto the user's own explicit decisions.
func TestPinnedSkillStillInstallsWhenAbsent(t *testing.T) {
	p := Reconcile(
		[]Cell{{Skill: "deploy", Source: "srcA", Target: "t1"}},
		[]AvailableSkill{{Name: "deploy", Source: "srcA", Fingerprint: "h1", Pinned: true}},
		nil,
		nil,
	)
	ops(t, p, 1)
	if p.Operations[0].Kind != Install {
		t.Errorf("kind = %q, want install", p.Operations[0].Kind)
	}
}

func TestPinnedSkillStillUninstallsWhenUnselected(t *testing.T) {
	installed := []domain.Installation{{SkillName: "deploy", SourceName: "srcA", TargetName: "t1", Fingerprint: "h1"}}
	p := Reconcile(nil, []AvailableSkill{{Name: "deploy", Source: "srcA", Fingerprint: "h1", Pinned: true}}, installed, nil)
	ops(t, p, 1)
	if p.Operations[0].Kind != Uninstall {
		t.Errorf("kind = %q, want uninstall", p.Operations[0].Kind)
	}
}

// Switching the Source for a name is an explicit user action, not upstream drift,
// so a Pin does not suppress it.
func TestPinnedSkillStillReinstallsOnSourceChange(t *testing.T) {
	installed := []domain.Installation{{SkillName: "deploy", SourceName: "srcA", TargetName: "t1", Fingerprint: "h1"}}
	p := Reconcile(
		[]Cell{{Skill: "deploy", Source: "srcB", Target: "t1"}},
		[]AvailableSkill{{Name: "deploy", Source: "srcB", Fingerprint: "h2", Pinned: true}},
		installed,
		nil,
	)
	ops(t, p, 1)
	if p.Operations[0].Reason != ReasonSourceChanged {
		t.Errorf("got %+v, want source-changed", p.Operations[0])
	}
}

// A re-pointed Target leaves the new path empty, so the copy must still be made:
// without it the Pin would freeze the row into having no files at all.
func TestPinnedSkillStillReinstallsOnTargetMove(t *testing.T) {
	installed := []domain.Installation{{
		SkillName: "deploy", SourceName: "srcA", TargetName: "t1",
		Path: "/old/path", Fingerprint: "h1",
	}}
	p := Reconcile(
		[]Cell{{Skill: "deploy", Source: "srcA", Target: "t1"}},
		[]AvailableSkill{{Name: "deploy", Source: "srcA", Fingerprint: "h1", Pinned: true}},
		installed,
		map[string]string{"t1": "/new/path"},
	)
	ops(t, p, 1)
	if p.Operations[0].Reason != ReasonTargetMoved {
		t.Errorf("got %+v, want target-moved", p.Operations[0])
	}
}

// A Pin is per (Skill, Source) row: pinning one Source's row must not freeze the
// same Skill coming from another Source.
func TestPinIsPerSourceRow(t *testing.T) {
	installed := []domain.Installation{
		{SkillName: "deploy", SourceName: "srcA", TargetName: "t1", Fingerprint: "old"},
		{SkillName: "deploy", SourceName: "srcB", TargetName: "t2", Fingerprint: "old"},
	}
	p := Reconcile(
		[]Cell{
			{Skill: "deploy", Source: "srcA", Target: "t1"},
			{Skill: "deploy", Source: "srcB", Target: "t2"},
		},
		[]AvailableSkill{
			{Name: "deploy", Source: "srcA", Fingerprint: "new", Pinned: true},
			{Name: "deploy", Source: "srcB", Fingerprint: "new"},
		},
		installed,
		nil,
	)
	ops(t, p, 1)
	if p.Operations[0].SourceName != "srcB" {
		t.Errorf("only the unpinned row should be reinstalled, got %+v", p.Operations[0])
	}
}
