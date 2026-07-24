package diff

import "strings"

// splitLines splits a file body into diffable lines. A single trailing newline is
// normalised away (so "a\n" and "a" yield the same one line) and an empty file
// yields no lines at all.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// Hunks groups the line-level edit script between old and new into unified-diff
// hunks, each carrying up to context unchanged lines on either side of its
// changes. Runs of changes closer than 2*context lines are merged into one hunk,
// as `git diff` does, so a screenful of edits doesn't fragment into slivers.
func Hunks(old, next []string, context int) []Hunk {
	if context < 0 {
		context = 0
	}
	script := diffSeq(old, next)

	// Number every scripted line on both sides: a 1-based line number where the
	// side has the line, and the count consumed so far where it does not (that is
	// what a zero-length hunk range reports).
	type numbered struct {
		line           Line
		oldNo, newNo   int
		oldSeen, newIn bool
	}
	rows := make([]numbered, 0, len(script))
	oldSeen, newSeen := 0, 0
	for _, l := range script {
		n := numbered{line: l}
		switch l.Kind {
		case Context:
			oldSeen, newSeen = oldSeen+1, newSeen+1
			n.oldNo, n.newNo = oldSeen, newSeen
			n.oldSeen, n.newIn = true, true
		case Del:
			oldSeen++
			n.oldNo, n.newNo = oldSeen, newSeen
			n.oldSeen = true
		case Add:
			newSeen++
			n.oldNo, n.newNo = oldSeen, newSeen
			n.newIn = true
		}
		rows = append(rows, n)
	}

	// Locate the change runs, then widen each by the context and merge the ones
	// whose contexts would touch.
	type span struct{ lo, hi int } // inclusive row indices
	var spans []span
	for i, r := range rows {
		if r.line.Kind == Context {
			continue
		}
		lo, hi := max(0, i-context), min(len(rows)-1, i+context)
		if n := len(spans); n > 0 && lo <= spans[n-1].hi+1 {
			spans[n-1].hi = max(spans[n-1].hi, hi)
			continue
		}
		spans = append(spans, span{lo, hi})
	}

	hunks := make([]Hunk, 0, len(spans))
	for _, sp := range spans {
		h := Hunk{Lines: make([]Line, 0, sp.hi-sp.lo+1)}
		for i := sp.lo; i <= sp.hi; i++ {
			r := rows[i]
			h.Lines = append(h.Lines, r.line)
			if r.oldSeen {
				if h.OldLines == 0 {
					h.OldStart = r.oldNo
				}
				h.OldLines++
			}
			if r.newIn {
				if h.NewLines == 0 {
					h.NewStart = r.newNo
				}
				h.NewLines++
			}
		}
		// A hunk with no line on one side is a pure insertion or deletion: report
		// the position it applies after, the same as a unified diff's "0" range.
		if h.OldLines == 0 {
			h.OldStart = rows[sp.lo].oldNo
		}
		if h.NewLines == 0 {
			h.NewStart = rows[sp.lo].newNo
		}
		hunks = append(hunks, h)
	}
	return hunks
}

// diffSeq returns the edit script that turns old into next: every line tagged
// Context, Del or Add, in reading order.
func diffSeq(old, next []string) []Line {
	out := make([]Line, 0, len(old)+len(next))
	appendDiff(old, next, &out)
	return out
}

// appendDiff appends the edit script for (a, b) to out. It strips the common
// prefix and suffix — the cheap, overwhelmingly common case for an edited Skill
// file — then splits the remainder at Myers' middle snake and recurses, which is
// what keeps memory linear in the input rather than quadratic.
func appendDiff(a, b []string, out *[]Line) {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	for _, l := range a[:p] {
		*out = append(*out, Line{Kind: Context, Text: l})
	}
	a, b = a[p:], b[p:]

	s := 0
	for s < len(a) && s < len(b) && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	suffix := a[len(a)-s:]
	a, b = a[:len(a)-s], b[:len(b)-s]

	switch {
	case len(a) == 0:
		appendAll(b, Add, out)
	case len(b) == 0:
		appendAll(a, Del, out)
	case len(a)+len(b) > maxDiffLines:
		// Past the cap the O(ND) search is not worth the wait: report the block as
		// replaced wholesale. Coarse, but instant and never wrong about *what* the
		// two sides contain.
		appendAll(a, Del, out)
		appendAll(b, Add, out)
	default:
		x, y, ok := bisect(a, b)
		if !ok {
			appendAll(a, Del, out)
			appendAll(b, Add, out)
			break
		}
		appendDiff(a[:x], b[:y], out)
		appendDiff(a[x:], b[y:], out)
	}

	for _, l := range suffix {
		*out = append(*out, Line{Kind: Context, Text: l})
	}
}

func appendAll(lines []string, kind LineKind, out *[]Line) {
	for _, l := range lines {
		*out = append(*out, Line{Kind: kind, Text: l})
	}
}

// bisect finds the point (x, y) where a forward and a reverse D-path of Myers'
// algorithm meet, so (a, b) can be split into two strictly smaller subproblems
// solved independently. It reports false when no meeting point is found within
// the search bound, or when the split would not shrink the problem — the caller
// then falls back to a wholesale replacement instead of recursing forever.
func bisect(a, b []string) (int, int, bool) {
	n, m := len(a), len(b)
	maxD := (n + m + 1) / 2
	offset := maxD
	length := 2*maxD + 2
	vf := make([]int, length) // furthest-reaching forward paths, by diagonal
	vr := make([]int, length) // …and reverse paths
	for i := range vf {
		vf[i], vr[i] = -1, -1
	}
	vf[offset+1] = 0
	vr[offset+1] = 0

	delta := n - m
	// When delta is odd the forward path is the one that can overlap a reverse
	// path of the same D; when it is even, the reverse path is.
	front := delta%2 != 0
	fStart, fEnd, rStart, rEnd := 0, 0, 0, 0

	for d := 0; d < maxD; d++ {
		for k := -d + fStart; k <= d-fEnd; k += 2 {
			kOff := offset + k
			var x int
			if k == -d || (k != d && vf[kOff-1] < vf[kOff+1]) {
				x = vf[kOff+1]
			} else {
				x = vf[kOff-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			vf[kOff] = x
			switch {
			case x > n:
				fEnd += 2
			case y > m:
				fStart += 2
			case front:
				rOff := offset + delta - k
				if rOff >= 0 && rOff < length && vr[rOff] != -1 {
					if x >= n-vr[rOff] { // the paths overlap: split here
						return split(x, y, n, m)
					}
				}
			}
		}

		for k := -d + rStart; k <= d-rEnd; k += 2 {
			kOff := offset + k
			var x int
			if k == -d || (k != d && vr[kOff-1] < vr[kOff+1]) {
				x = vr[kOff+1]
			} else {
				x = vr[kOff-1] + 1
			}
			y := x - k
			for x < n && y < m && a[n-x-1] == b[m-y-1] {
				x, y = x+1, y+1
			}
			vr[kOff] = x
			switch {
			case x > n:
				rEnd += 2
			case y > m:
				rStart += 2
			case !front:
				fOff := offset + delta - k
				if fOff >= 0 && fOff < length && vf[fOff] != -1 {
					fx := vf[fOff]
					fy := fx - (fOff - offset)
					if fx >= n-x {
						return split(fx, fy, n, m)
					}
				}
			}
		}
	}
	return 0, 0, false
}

// split validates a candidate middle-snake point: it is only usable if both
// halves are strictly smaller than the whole, otherwise recursing on it would
// never terminate.
func split(x, y, n, m int) (int, int, bool) {
	if x < 0 || y < 0 || x > n || y > m {
		return 0, 0, false
	}
	if (x == 0 && y == 0) || (x == n && y == m) {
		return 0, 0, false
	}
	return x, y, true
}
