package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

// driftedModel installs the fixture skill into target "cc", then moves the Source
// on — the exact state that makes "update available" say something changed
// without saying what.
func driftedModel(t *testing.T) (Model, *engine.Engine) {
	t.Helper()
	e := testEngine(t, "cc")
	cat := e.Refresh()
	desired := []reconcile.Cell{{Skill: "deploy", Source: "local", Target: "cc"}}
	rep, err := e.Apply(e.Preview(desired, cat), apply.Options{})
	if err != nil || !rep.AllOK() {
		t.Fatalf("install failed: err=%v rep=%+v", err, rep)
	}
	mustWrite(t, filepath.Join(cat.Skills[0].Dir, "SKILL.md"),
		"---\nname: deploy\ndescription: d\n---\nv2\n")
	mustWrite(t, filepath.Join(cat.Skills[0].Dir, "run.sh"), "echo go\n")

	m := New(e).onRefreshed(e.Refresh())
	m.width, m.height = 100, 30
	return m, e
}

// settle runs the command a keystroke produced and folds its message back in, the
// way the Bubble Tea runtime would.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command to run off-loop")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func TestDiffFromSkillViewShowsUpstreamChanges(t *testing.T) {
	m, _ := driftedModel(t)

	m = applyKeys(m, runes("v")) // open the skill explorer
	tm, cmd := m.Update(runes("d"))
	m = tm.(Model)
	if m.mode != modeDiff {
		t.Fatalf("'d' should open the diff, got mode %v (msg %q)", m.mode, m.viewMsg)
	}
	if !m.diffLoading {
		t.Fatal("the comparison should start off-loop, leaving the screen loading")
	}
	m = settle(t, m, cmd)

	if m.diffLoading || m.diffErr != nil {
		t.Fatalf("diff should have landed cleanly: loading=%v err=%v", m.diffLoading, m.diffErr)
	}
	c := m.diffComp
	if !c.Tracked || !c.Pristine {
		t.Errorf("an untouched install should read as tracked and pristine: %+v", c)
	}
	added, _, modified := c.Summary.Counts()
	if added != 1 || modified != 1 {
		t.Fatalf("expected one added and one modified file, got %+v", c.Summary.Changes)
	}
	out := m.View()
	for _, want := range []string{"SKILL.md", "run.sh", "+v2", "-v1", "@@"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff view should contain %q; got:\n%s", want, out)
		}
	}

	// esc returns to the skill explorer it was opened from.
	esc, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if esc.(Model).mode != modeSkillTree {
		t.Fatalf("esc from the diff should return to the tree, got %v", esc.(Model).mode)
	}
}

func TestDiffFromSkillViewWithoutInstalledCopyIsInline(t *testing.T) {
	e := testEngine(t, "cc") // nothing installed
	m := New(e).onRefreshed(e.Refresh())
	m = applyKeys(m, runes("v"))

	tm, cmd := m.Update(runes("d"))
	m = tm.(Model)
	if m.mode != modeSkillTree || cmd != nil {
		t.Fatalf("with no installed copy 'd' should stay put, got mode %v cmd=%v", m.mode, cmd != nil)
	}
	if !strings.Contains(m.viewMsg, "nothing to compare") {
		t.Fatalf("expected an inline explanation, got %q", m.viewMsg)
	}
}

func TestDiffOfPristineInstallSaysSo(t *testing.T) {
	e := testEngine(t, "cc")
	cat := e.Refresh()
	desired := []reconcile.Cell{{Skill: "deploy", Source: "local", Target: "cc"}}
	if _, err := e.Apply(e.Preview(desired, cat), apply.Options{}); err != nil {
		t.Fatal(err)
	}
	m := New(e).onRefreshed(e.Refresh())
	m.width, m.height = 100, 30

	m = applyKeys(m, runes("v"))
	tm, cmd := m.Update(runes("d"))
	m = settle(t, tm.(Model), cmd)
	if !m.diffComp.Summary.Empty() {
		t.Fatalf("a just-installed copy should show no differences: %+v", m.diffComp.Summary.Changes)
	}
	if !strings.Contains(m.View(), "no differences") {
		t.Fatalf("expected a 'no differences' note; got:\n%s", m.View())
	}
}

// A hand-edited copy must be flagged: the diff then mixes local and upstream
// changes, so the caveat is what keeps the reading honest.
func TestDiffFlagsLocallyModifiedCopy(t *testing.T) {
	m, e := driftedModel(t)
	dir, ok := e.InstalledCopy("cc", "deploy")
	if !ok {
		t.Fatal("fixture should be installed")
	}
	mustWrite(t, filepath.Join(dir, "SKILL.md"), "hand edit\n")

	m = applyKeys(m, runes("v"))
	tm, cmd := m.Update(runes("d"))
	m = settle(t, tm.(Model), cmd)
	if m.diffComp.Pristine {
		t.Fatal("a hand-edited copy must not read as pristine")
	}
	if !strings.Contains(m.View(), "modified locally") {
		t.Fatalf("expected the local-drift caveat; got:\n%s", m.View())
	}
}

// A comparison that lands after the user has left must not yank them back.
func TestDiffStaleResultIgnored(t *testing.T) {
	m, _ := driftedModel(t)
	m = applyKeys(m, runes("v"))
	tm, cmd := m.Update(runes("d"))
	m = tm.(Model)

	esc, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = esc.(Model)
	late, _ := m.Update(cmd())
	if late.(Model).mode != modeSkillTree {
		t.Fatalf("a late comparison should be ignored; mode = %v", late.(Model).mode)
	}
}

func TestDiffFromPlanCursor(t *testing.T) {
	m, _ := driftedModel(t)

	m = applyKeys(m, runes("p"))
	if m.mode != modePlan {
		t.Fatalf("expected the plan, got %v", m.mode)
	}
	ops := m.preview.Plan.Operations
	if len(ops) != 1 || ops[0].Kind != reconcile.Reinstall {
		t.Fatalf("expected a single reinstall, got %+v", ops)
	}
	if !strings.Contains(m.View(), "diff") {
		t.Errorf("the plan footer should offer the diff key; got:\n%s", m.View())
	}

	tm, cmd := m.Update(runes("d"))
	m = tm.(Model)
	if m.mode != modeDiff {
		t.Fatalf("'d' on a reinstall should open the diff, got %v (msg %q)", m.mode, m.planMsg)
	}
	m = settle(t, m, cmd)
	if m.diffErr != nil || m.diffComp.Summary.Empty() {
		t.Fatalf("expected the pending reinstall's changes: err=%v summary=%+v", m.diffErr, m.diffComp.Summary)
	}
	// esc returns to the Plan, so the user can go on to apply it.
	esc, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if esc.(Model).mode != modePlan {
		t.Fatalf("esc from a plan-opened diff should return to the plan, got %v", esc.(Model).mode)
	}
}

func TestPlanDiffOnFreshInstallIsInline(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e).onRefreshed(e.Refresh())
	m.width, m.height = 100, 30
	m = applyKeys(m, runes(" "), runes("p")) // select the skill, open the plan

	if op, ok := m.curPlanOp(); !ok || op.Kind != reconcile.Install {
		t.Fatalf("expected an install under the cursor, got %+v ok=%v", op, ok)
	}
	tm, cmd := m.Update(runes("d"))
	m = tm.(Model)
	if m.mode != modePlan || cmd != nil {
		t.Fatalf("a fresh install has no old side; 'd' should stay in the plan (mode %v)", m.mode)
	}
	if !strings.Contains(m.planMsg, "nothing to compare") {
		t.Fatalf("expected an inline explanation, got %q", m.planMsg)
	}
	if !strings.Contains(m.View(), "nothing to compare") {
		t.Fatalf("the note should be visible in the plan; got:\n%s", m.View())
	}
}

// The Plan's cursor must stay on a real operation and window itself to the
// terminal height, so a long plan is navigable rather than overflowing.
func TestPlanCursorNavigationAndWindow(t *testing.T) {
	e := testEngineSkills(t, "a", "b", "c", "d", "e")
	m := New(e).onRefreshed(e.Refresh())
	// A height that fits fewer rows than there are operations, so the window has
	// to move with the cursor.
	m.width, m.height = 100, 12
	m = applyKeys(m, runes("a"), runes("j"), runes("a"), runes("j"), runes("a"),
		runes("j"), runes("a"), runes("j"), runes("a"))
	m = applyKeys(m, runes("p"))
	if len(m.preview.Plan.Operations) != 5 {
		t.Fatalf("expected 5 installs, got %+v", m.preview.Plan.Operations)
	}
	if vis := m.planVisibleOps(); vis >= 5 {
		t.Fatalf("test needs a windowed plan; %d of 5 rows fit", vis)
	}

	// Down past the end clamps to the last operation and scrolls it into view…
	for i := 0; i < 10; i++ {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = u.(Model)
	}
	if m.planCursor != 4 {
		t.Fatalf("cursor should clamp to the last operation, got %d", m.planCursor)
	}
	if vis := m.planVisibleOps(); m.planScroll == 0 || m.planCursor >= m.planScroll+vis {
		t.Fatalf("cursor %d outside window [%d, %d)", m.planCursor, m.planScroll, m.planScroll+vis)
	}
	if !strings.Contains(m.View(), "to scroll") {
		t.Fatalf("a windowed plan should say it scrolls; got:\n%s", m.View())
	}
	// …and up past the start clamps to the first.
	for i := 0; i < 10; i++ {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = u.(Model)
	}
	if m.planCursor != 0 || m.planScroll != 0 {
		t.Fatalf("cursor should clamp to the first operation, got cursor=%d scroll=%d", m.planCursor, m.planScroll)
	}
}

func TestDiffViewRendersWhileLoadingWithoutPanic(t *testing.T) {
	m, _ := driftedModel(t)
	m = applyKeys(m, runes("v"))
	tm, _ := m.Update(runes("d"))
	m = tm.(Model)
	if out := m.View(); !strings.Contains(out, "comparing") {
		t.Fatalf("the loading diff should say so; got:\n%s", out)
	}
}

func TestDiffWithNoTargetsIsInline(t *testing.T) {
	m, _ := driftedModel(t)
	m = applyKeys(m, runes("v"))
	m.targets = nil // no column to compare against
	tm, cmd := m.Update(runes("d"))
	m = tm.(Model)
	if m.mode != modeSkillTree || cmd != nil {
		t.Fatalf("with no targets 'd' should stay put, got mode %v", m.mode)
	}
	if !strings.Contains(m.viewMsg, "no target") {
		t.Fatalf("expected an inline explanation, got %q", m.viewMsg)
	}
}

// A comparison that cannot be made (here: a Target that vanished from the Config
// between opening the diff and running it) must be reported on the screen, never
// crash or render blank.
func TestDiffErrorIsReportedNotFatal(t *testing.T) {
	m, _ := driftedModel(t)
	m = applyKeys(m, runes("v"))
	m, cmd := m.enterDiff(m.viewSkill, "gone", modeSkillTree)
	m = settle(t, m, cmd)

	if m.diffErr == nil {
		t.Fatal("expected the comparison to fail")
	}
	if out := m.View(); !strings.Contains(out, "could not compare") {
		t.Fatalf("the diff screen should report the failure; got:\n%s", out)
	}
}
