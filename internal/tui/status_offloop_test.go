package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
)

// TestRefreshCmdCarriesStatus confirms the background refresh command computes
// the cell Statuses itself, so the UI loop never fingerprints installed copies
// (skillmux-c1e).
func TestRefreshCmdCarriesStatus(t *testing.T) {
	m, _ := envWithModified(t)
	msg, ok := refreshCmd(m.eng)().(refreshDoneMsg)
	if !ok {
		t.Fatal("refreshCmd should produce a refreshDoneMsg")
	}
	var found bool
	for _, c := range msg.cells {
		if c.SkillName == "deploy" && c.SourceName == "local" && c.TargetName == "cc" {
			found = true
			if c.Status != domain.StatusModified {
				t.Errorf("cell status = %v, want %v", c.Status, domain.StatusModified)
			}
		}
	}
	if !found {
		t.Fatalf("refreshDoneMsg carries no cell for deploy/local/cc: %+v", msg.cells)
	}
}

// TestRefreshDoneRendersPrecomputedStatus confirms a landed Refresh renders the
// Statuses carried in the message rather than recomputing them on the UI loop:
// the filesystem changes after the message is built, yet the message's cells win.
func TestRefreshDoneRendersPrecomputedStatus(t *testing.T) {
	m, targetPath := envWithModified(t)
	m.refreshing = true
	msg := refreshCmd(m.eng)().(refreshDoneMsg)

	// Remove the installed copy: recomputing now would no longer say "modified".
	if err := os.RemoveAll(filepath.Join(targetPath, "deploy")); err != nil {
		t.Fatal(err)
	}
	upd, _ := m.Update(msg)
	m = upd.(Model)
	if m.refreshing {
		t.Error("refreshing should clear once the Refresh lands")
	}
	if st := m.status[statusKey{"deploy", "local", "cc"}]; st != domain.StatusModified {
		t.Fatalf("status = %v, want the precomputed %v", st, domain.StatusModified)
	}

	// Cells supplied in the message are taken as-is.
	msg.cells = []engine.CellStatus{{SkillName: "deploy", SourceName: "local", TargetName: "cc", Status: domain.StatusPinned}}
	m.refreshing = true
	upd, _ = m.Update(msg)
	m = upd.(Model)
	if st := m.status[statusKey{"deploy", "local", "cc"}]; st != domain.StatusPinned {
		t.Fatalf("status = %v, want the supplied %v", st, domain.StatusPinned)
	}
}
