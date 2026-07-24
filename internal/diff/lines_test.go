package diff

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// render turns hunks into a compact unified-diff text, so tests can assert on
// the shape a reader would actually see.
func render(hunks []Hunk) string {
	var b strings.Builder
	for _, h := range hunks {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
		for _, l := range h.Lines {
			switch l.Kind {
			case Add:
				b.WriteString("+" + l.Text + "\n")
			case Del:
				b.WriteString("-" + l.Text + "\n")
			default:
				b.WriteString(" " + l.Text + "\n")
			}
		}
	}
	return b.String()
}

func lines(s string) []string { return splitLines(s) }

func TestSplitLinesNormalisesTrailingNewline(t *testing.T) {
	if got := splitLines(""); got != nil {
		t.Fatalf("empty file should yield no lines, got %q", got)
	}
	a, b := splitLines("x\n"), splitLines("x")
	if len(a) != 1 || len(b) != 1 || a[0] != b[0] {
		t.Fatalf("trailing newline should not change the lines: %q vs %q", a, b)
	}
	if got := splitLines("a\nb\n"); len(got) != 2 {
		t.Fatalf("expected 2 lines, got %q", got)
	}
}

func TestHunksSingleLineChange(t *testing.T) {
	old := lines("one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n")
	next := lines("one\ntwo\nthree\nfour\nFIVE\nsix\nseven\neight\n")
	got := render(Hunks(old, next, 2))
	want := "@@ -3,5 +3,5 @@\n three\n four\n-five\n+FIVE\n six\n seven\n"
	if got != want {
		t.Fatalf("hunk mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestHunksAppendAtEnd(t *testing.T) {
	old := lines("a\nb\n")
	next := lines("a\nb\nc\n")
	got := render(Hunks(old, next, 3))
	want := "@@ -1,2 +1,3 @@\n a\n b\n+c\n"
	if got != want {
		t.Fatalf("hunk mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestHunksDeletionOnly(t *testing.T) {
	old := lines("a\nb\nc\n")
	next := lines("a\nc\n")
	got := render(Hunks(old, next, 1))
	want := "@@ -1,3 +1,2 @@\n a\n-b\n c\n"
	if got != want {
		t.Fatalf("hunk mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Two distant edits must land in separate hunks, and adjacent ones must merge
// into a single hunk (the 2*context rule).
func TestHunksSplitAndMerge(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&oldB, "line %d\n", i)
		switch i {
		case 2, 4:
			fmt.Fprintf(&newB, "LINE %d\n", i) // close together: one hunk
		case 25:
			fmt.Fprintf(&newB, "LINE %d\n", i) // far away: its own hunk
		default:
			fmt.Fprintf(&newB, "line %d\n", i)
		}
	}
	hunks := Hunks(lines(oldB.String()), lines(newB.String()), 3)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d:\n%s", len(hunks), render(hunks))
	}
	if hunks[0].OldStart != 1 || hunks[1].OldStart != 22 {
		t.Fatalf("unexpected hunk starts: %+v / %+v", hunks[0], hunks[1])
	}
}

func TestHunksNoChangeIsEmpty(t *testing.T) {
	same := lines("a\nb\nc\n")
	if h := Hunks(same, same, 3); len(h) != 0 {
		t.Fatalf("identical input should produce no hunks, got %s", render(h))
	}
}

func TestHunksWholeFileReplacement(t *testing.T) {
	got := render(Hunks(lines("a\nb\n"), lines("x\ny\n"), 3))
	want := "@@ -1,2 +1,2 @@\n-a\n-b\n+x\n+y\n"
	if got != want {
		t.Fatalf("hunk mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestHunksFromEmptyOldSide(t *testing.T) {
	got := render(Hunks(nil, lines("a\nb\n"), 3))
	want := "@@ -0,0 +1,2 @@\n+a\n+b\n"
	if got != want {
		t.Fatalf("hunk mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// The edit script must be a valid, minimal-enough transcript: replaying its
// Context+Del lines reproduces the old side, and Context+Add the new side. This
// is the invariant that keeps the Myers bisect honest, so it is fuzzed over many
// random inputs rather than a handful of examples.
func TestDiffSeqReconstructsBothSides(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	pool := []string{"alpha", "beta", "gamma", "delta", "epsilon", "", "  indented"}
	for c := 0; c < 400; c++ {
		old := randLines(rng, pool, rng.Intn(40))
		next := randLines(rng, pool, rng.Intn(40))
		script := diffSeq(old, next)

		var gotOld, gotNew []string
		for _, l := range script {
			switch l.Kind {
			case Context:
				gotOld = append(gotOld, l.Text)
				gotNew = append(gotNew, l.Text)
			case Del:
				gotOld = append(gotOld, l.Text)
			case Add:
				gotNew = append(gotNew, l.Text)
			}
		}
		if !equalLines(gotOld, old) {
			t.Fatalf("case %d: context+del = %q, want old %q", c, gotOld, old)
		}
		if !equalLines(gotNew, next) {
			t.Fatalf("case %d: context+add = %q, want new %q", c, gotNew, next)
		}
	}
}

// A one-line edit inside a large file must be found as a one-line edit, not a
// wholesale replacement — the property that makes the diff worth reading.
func TestDiffSeqIsMinimalOnLargeFile(t *testing.T) {
	old := make([]string, 2000)
	for i := range old {
		old[i] = fmt.Sprintf("line %d", i)
	}
	next := append([]string(nil), old...)
	next[1000] = "CHANGED"

	adds, dels := 0, 0
	for _, l := range diffSeq(old, next) {
		switch l.Kind {
		case Add:
			adds++
		case Del:
			dels++
		}
	}
	if adds != 1 || dels != 1 {
		t.Fatalf("expected a 1-line edit, got %d adds / %d dels", adds, dels)
	}
}

// Past maxDiffLines the search bails out to a wholesale replacement: still a
// correct transcript of both sides, just coarse — and fast.
func TestDiffSeqFallsBackAboveLineCap(t *testing.T) {
	old := make([]string, maxDiffLines)
	next := make([]string, maxDiffLines)
	for i := range old {
		old[i] = fmt.Sprintf("old %d", i)
		next[i] = fmt.Sprintf("new %d", i)
	}
	script := diffSeq(old, next)
	if len(script) != 2*maxDiffLines {
		t.Fatalf("fallback should transcribe both sides in full, got %d lines", len(script))
	}
	if script[0].Kind != Del || script[len(script)-1].Kind != Add {
		t.Fatalf("fallback should be all deletions then all additions: %+v … %+v",
			script[0], script[len(script)-1])
	}
}

func randLines(rng *rand.Rand, pool []string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = pool[rng.Intn(len(pool))]
	}
	return out
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
