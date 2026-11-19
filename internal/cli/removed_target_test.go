package cli

import (
	"strings"
	"testing"
)

// After a Target is removed, headless apply must not try to uninstall its
// stale Installations (skillmux-fq2).
func TestApplyAfterTargetRemovedIsClean(t *testing.T) {
	e, _, _ := newEnv(t)
	install(t, e)
	if err := e.RemoveTarget("cc"); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(e, "", "apply", "--yes")
	if code != exitOK || !strings.Contains(out, "Nothing to do.") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

// Same when the Target was dropped from the Config by hand.
func TestApplyAfterTargetHandRemovedIsClean(t *testing.T) {
	e, _, _ := newEnv(t)
	install(t, e)
	e.Config.Targets = nil
	code, out, errOut := run(e, "", "apply", "--yes")
	if code != exitOK || !strings.Contains(out, "Nothing to do.") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}
