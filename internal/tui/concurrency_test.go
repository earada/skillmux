package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestLeaveConfigCoalescesWhileRefreshing reproduces the startup-refresh +
// config-exit case: New leaves a background Refresh in flight, and leaving
// config must queue a re-scan rather than launch a second, concurrent Refresh
// (skillmux-3vj). The queued one runs only once the first completes.
func TestLeaveConfigCoalescesWhileRefreshing(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e) // startup Refresh in flight
	if !m.refreshing {
		t.Fatal("New should leave a background refresh in flight")
	}
	m.mode = modeConfig

	upd, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // leaveConfig
	m = upd.(Model)
	if m.mode != modeMatrix {
		t.Fatalf("esc should return to matrix, got %v", m.mode)
	}
	if cmd != nil {
		t.Fatal("must not start a second Refresh while one is in flight")
	}
	if !m.pendingRefresh {
		t.Fatal("the re-scan should be queued as pending")
	}

	// The in-flight refresh lands: the coalesced one now runs.
	upd, cmd = m.Update(refreshDoneMsg{cat: e.Refresh()})
	m = upd.(Model)
	if cmd == nil {
		t.Fatal("queued refresh should run once the first completes")
	}
	if !m.refreshing || m.pendingRefresh {
		t.Fatalf("coalesced refresh should be the only one in flight (refreshing=%v pending=%v)", m.refreshing, m.pendingRefresh)
	}
}

// TestRepeatedRefreshCoalesces ensures a burst of 'r' presses never launches
// overlapping Refreshes: the first starts one, the rest only set pending.
func TestRepeatedRefreshCoalesces(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e).onRefreshed(e.Refresh()) // idle

	upd, cmd := m.Update(runes("r")) // starts a refresh
	m = upd.(Model)
	if cmd == nil || !m.refreshing {
		t.Fatal("'r' should start a refresh when idle")
	}
	upd, cmd = m.Update(runes("r")) // already refreshing: coalesce
	m = upd.(Model)
	if cmd != nil {
		t.Fatal("a second 'r' must not launch a concurrent refresh")
	}
	if !m.pendingRefresh {
		t.Fatal("the extra refresh request should be queued")
	}
}

// TestRepeatedApplyIsBlocked confirms a second Apply cannot start while one is
// in flight: after confirming an Apply the matrix refuses to reopen the Plan.
func TestRepeatedApplyIsBlocked(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e).onRefreshed(e.Refresh()) // idle
	if m.busy() {
		t.Fatal("model should be idle after the startup refresh lands")
	}

	m = applyKeys(m, runes(" ")) // select deploy -> cc
	upd, _ := m.Update(runes("p"))
	m = upd.(Model)
	if m.mode != modePlan {
		t.Fatalf("expected plan screen, got %v", m.mode)
	}
	upd, cmd := m.Update(runes("y")) // confirm apply
	m = upd.(Model)
	if !m.applying || cmd == nil {
		t.Fatal("confirming should dispatch an Apply and flag applying")
	}
	if m.mode != modeMatrix {
		t.Fatalf("apply returns to the matrix, got %v", m.mode)
	}

	// While the Apply is in flight, opening the Plan again is refused.
	upd, _ = m.Update(runes("p"))
	m = upd.(Model)
	if m.mode == modePlan {
		t.Fatal("must not open the Plan while an Apply is in flight")
	}
}

// TestConfigKeyDeferredWhileRefreshing reproduces skillmux-dkq: pressing 'c'
// during the startup Refresh must not silently drop the key (which reads as a
// freeze). Instead it queues the intent and opens the config the moment the
// Refresh lands.
func TestConfigKeyDeferredWhileRefreshing(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e) // startup Refresh in flight
	if !m.refreshing {
		t.Fatal("New should leave a background refresh in flight")
	}

	upd, _ := m.Update(runes("c"))
	m = upd.(Model)
	if m.mode == modeConfig {
		t.Fatal("config must not open mid-Refresh (Config is being scanned)")
	}
	if !m.pendingConfig {
		t.Fatal("'c' during a Refresh should queue the config-open, not drop it")
	}

	// The Refresh lands: the deferred config-open now runs.
	upd, _ = m.Update(refreshDoneMsg{cat: e.Refresh()})
	m = upd.(Model)
	if m.mode != modeConfig {
		t.Fatalf("config should open once the Refresh lands, got mode %v", m.mode)
	}
	if m.pendingConfig {
		t.Fatal("pendingConfig should clear once the config opens")
	}
}

// TestPlanKeyDeferredWhileRefreshing mirrors the config case for p/enter: the
// key is queued during the startup Refresh and the Plan opens (against the
// fresh catalog) once the Refresh lands, rather than being dropped (skillmux-dkq).
func TestPlanKeyDeferredWhileRefreshing(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e) // startup Refresh in flight

	upd, _ := m.Update(runes("p"))
	m = upd.(Model)
	if m.mode == modePlan {
		t.Fatal("Plan must not open mid-Refresh (catalog is being rewritten)")
	}
	if !m.pendingPlan {
		t.Fatal("'p' during a Refresh should queue the Plan-open, not drop it")
	}

	upd, _ = m.Update(refreshDoneMsg{cat: e.Refresh()})
	m = upd.(Model)
	if m.mode != modePlan {
		t.Fatalf("Plan should open once the Refresh lands, got mode %v", m.mode)
	}
	if m.pendingPlan {
		t.Fatal("pendingPlan should clear once the Plan opens")
	}
}

// TestLatestDeferredIntentWins confirms 'c' then 'p' (or vice versa) during a
// Refresh resolves to the last key pressed, never opening both.
func TestLatestDeferredIntentWins(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e) // startup Refresh in flight

	upd, _ := m.Update(runes("c")) // queue config…
	m = upd.(Model)
	upd, _ = m.Update(runes("p")) // …then override with plan
	m = upd.(Model)
	if m.pendingConfig || !m.pendingPlan {
		t.Fatalf("latest intent should win: pendingConfig=%v pendingPlan=%v", m.pendingConfig, m.pendingPlan)
	}

	upd, _ = m.Update(refreshDoneMsg{cat: e.Refresh()})
	m = upd.(Model)
	if m.mode != modePlan {
		t.Fatalf("the last-pressed key (p) should decide the view, got %v", m.mode)
	}
}

// TestConfigAndPlanKeysBlockedWhileBusy confirms the matrix refuses to enter
// config or open the Plan while a command is in flight, so a config edit never
// races a running Refresh and no Apply starts off in-flight state.
func TestConfigAndPlanKeysBlockedWhileBusy(t *testing.T) {
	e := testEngine(t, "cc")
	m := New(e) // startup Refresh in flight

	upd, _ := m.Update(runes("c"))
	if upd.(Model).mode == modeConfig {
		t.Error("config must not open while a command is in flight")
	}
	upd, _ = m.Update(runes("p"))
	if upd.(Model).mode == modePlan {
		t.Error("the Plan must not open while a command is in flight")
	}
}
