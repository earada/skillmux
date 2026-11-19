package tui

import (
	"testing"

	"github.com/earada/skillmux/internal/reconcile"
)

func TestRenameDesiredFollowsTheNewName(t *testing.T) {
	d := map[reconcile.Cell]bool{
		{Skill: "a", Source: "s", Target: "old"}:   true,
		{Skill: "b", Source: "s", Target: "old"}:   false,
		{Skill: "a", Source: "s", Target: "other"}: true,
	}
	renameDesired(d, "old", "new", func(c *reconcile.Cell) *string { return &c.Target })
	want := map[reconcile.Cell]bool{
		{Skill: "a", Source: "s", Target: "new"}:   true,
		{Skill: "b", Source: "s", Target: "new"}:   false,
		{Skill: "a", Source: "s", Target: "other"}: true,
	}
	if len(d) != len(want) {
		t.Fatalf("got %+v", d)
	}
	for c, on := range want {
		if got, ok := d[c]; !ok || got != on {
			t.Fatalf("got %+v, want %+v", d, want)
		}
	}
	renameDesired(d, "s", "crew", func(c *reconcile.Cell) *string { return &c.Source })
	for c := range d {
		if c.Source != "crew" {
			t.Fatalf("source not re-keyed: %+v", d)
		}
	}
}
