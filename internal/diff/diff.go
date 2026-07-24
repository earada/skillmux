// Package diff answers "what would a reinstall actually change?" for a Skill:
// it compares two Skill folders and reports the file-level changes (added,
// removed, modified) plus, per text file, the unified line hunks.
//
// The old side is the copy installed in a Target, the new side the Skill's
// current folder in its Source cache — see ADR 0007. It deliberately mirrors
// the fingerprint's notion of Skill content (regular files only, symlinks and
// specials skipped) so a cell flagged as drifted can never render an empty diff.
package diff

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"
)

// maxFileSize caps how large a file may be before its content diff is skipped
// (the change is still reported, with a Note). Skill files are tiny in practice;
// this bounds the read+diff so a pathological file cannot exhaust memory.
const maxFileSize = 1 << 20 // 1 MiB

// maxDiffLines caps the input the O(ND) line search runs on. Past it the two
// sides are reported as a wholesale replacement instead, so a huge generated
// file degrades to a coarse-but-instant diff rather than a stalled screen.
const maxDiffLines = 8000

// defaultContext is how many unchanged lines surround each change in a hunk.
const defaultContext = 3

// Kind is how a file differs between the two folders.
type Kind string

const (
	// Added is a file present only in the new folder.
	Added Kind = "added"
	// Removed is a file present only in the old folder.
	Removed Kind = "removed"
	// Modified is a file present in both whose bytes differ.
	Modified Kind = "modified"
)

// LineKind classifies one line of a content diff.
type LineKind int

const (
	// Context is a line both sides share.
	Context LineKind = iota
	// Add is a line only the new side has.
	Add
	// Del is a line only the old side has.
	Del
)

// Line is one line of a content diff.
type Line struct {
	Kind LineKind
	Text string
}

// Hunk is a contiguous run of changed lines with its surrounding context, with
// the 1-based start line and line count on each side (unified-diff semantics:
// a zero count means the change is an insertion/deletion at that position).
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []Line
}

// FileChange is one file that differs between the two folders. Hunks carry the
// content diff; when it could not be computed (binary, oversized, unreadable)
// Hunks is empty and Note says why.
type FileChange struct {
	// Path is the file's path relative to the Skill folder, slash-normalised.
	Path string
	Kind Kind
	// Adds and Dels count the added and deleted lines across Hunks.
	Adds, Dels int
	Hunks      []Hunk
	// Note explains an absent content diff; empty when Hunks tell the story.
	Note string
}

// Summary is the whole folder comparison: every differing file, ordered by path.
type Summary struct {
	Changes []FileChange
}

// Empty reports whether the two folders hold identical content.
func (s Summary) Empty() bool { return len(s.Changes) == 0 }

// Counts tallies the Summary by Kind, for the one-line headline above a diff.
func (s Summary) Counts() (added, removed, modified int) {
	for _, c := range s.Changes {
		switch c.Kind {
		case Added:
			added++
		case Removed:
			removed++
		case Modified:
			modified++
		}
	}
	return added, removed, modified
}

// Compare reports every file that differs between oldDir and newDir. Both must
// exist and be directories — a missing side is an error rather than "everything
// was added", since silently diffing against nothing would misreport the scale
// of a change. Identical files are omitted; the result is sorted by path.
func Compare(oldDir, newDir string) (Summary, error) {
	oldFiles, err := listFiles(oldDir)
	if err != nil {
		return Summary{}, err
	}
	newFiles, err := listFiles(newDir)
	if err != nil {
		return Summary{}, err
	}

	paths := make([]string, 0, len(oldFiles)+len(newFiles))
	for p := range oldFiles {
		paths = append(paths, p)
	}
	for p := range newFiles {
		if _, both := oldFiles[p]; !both {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	var s Summary
	for _, p := range paths {
		oldSize, inOld := oldFiles[p]
		newSize, inNew := newFiles[p]
		oldPath := filepath.Join(oldDir, filepath.FromSlash(p))
		newPath := filepath.Join(newDir, filepath.FromSlash(p))
		switch {
		case !inOld:
			s.Changes = append(s.Changes, wholeFileChange(p, Added, newPath, newSize))
		case !inNew:
			s.Changes = append(s.Changes, wholeFileChange(p, Removed, oldPath, oldSize))
		default:
			ch, differs := compareFile(p, oldPath, newPath, oldSize, newSize)
			if differs {
				s.Changes = append(s.Changes, ch)
			}
		}
	}
	return s, nil
}

// listFiles maps every regular file under dir to its size, keyed by its
// slash-normalised path relative to dir. Directories, symlinks and specials are
// skipped, matching what the fingerprint hashes.
func listFiles(dir string) (map[string]int64, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	out := map[string]int64{}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = fi.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// wholeFileChange describes a file that exists on only one side: its whole
// content is one hunk of additions (Added) or deletions (Removed), so the user
// can read what arrives or disappears without opening it separately.
func wholeFileChange(rel string, kind Kind, path string, size int64) FileChange {
	ch := FileChange{Path: rel, Kind: kind}
	text, note := readText(path, size)
	if note != "" {
		ch.Note = note
		return ch
	}
	lines := splitLines(text)
	if len(lines) == 0 {
		ch.Note = "empty file"
		return ch
	}
	lineKind := Add
	if kind == Removed {
		lineKind = Del
	}
	hunk := Hunk{Lines: make([]Line, 0, len(lines))}
	for _, l := range lines {
		hunk.Lines = append(hunk.Lines, Line{Kind: lineKind, Text: l})
	}
	if kind == Added {
		hunk.NewStart, hunk.NewLines = 1, len(lines)
		ch.Adds = len(lines)
	} else {
		hunk.OldStart, hunk.OldLines = 1, len(lines)
		ch.Dels = len(lines)
	}
	ch.Hunks = []Hunk{hunk}
	return ch
}

// compareFile compares one path present on both sides. It reports whether the
// bytes differ at all and, when they do, the FileChange describing how.
func compareFile(rel, oldPath, newPath string, oldSize, newSize int64) (FileChange, bool) {
	if oldSize == newSize {
		same, err := sameContent(oldPath, newPath)
		if err == nil && same {
			return FileChange{}, false
		}
		if err != nil {
			// Unreadable on one side: report the file as modified with the reason
			// rather than claiming it is unchanged.
			return FileChange{Path: rel, Kind: Modified, Note: "could not read: " + err.Error()}, true
		}
	}

	ch := FileChange{Path: rel, Kind: Modified}
	oldText, note := readText(oldPath, oldSize)
	if note != "" {
		ch.Note = note
		return ch, true
	}
	newText, note := readText(newPath, newSize)
	if note != "" {
		ch.Note = note
		return ch, true
	}
	ch.Hunks = Hunks(splitLines(oldText), splitLines(newText), defaultContext)
	for _, h := range ch.Hunks {
		for _, l := range h.Lines {
			switch l.Kind {
			case Add:
				ch.Adds++
			case Del:
				ch.Dels++
			}
		}
	}
	if len(ch.Hunks) == 0 {
		// The bytes differ but the lines do not: the only difference is the
		// trailing newline, which the line split deliberately normalises away.
		ch.Note = "differs only in the trailing newline"
	}
	return ch, true
}

// readText reads a file for diffing, returning a note instead of content when it
// is oversized, binary, or unreadable — the three cases with no useful line diff.
func readText(path string, size int64) (text, note string) {
	if size > maxFileSize {
		return "", fmt.Sprintf("too large to diff (%d bytes)", size)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "could not read: " + err.Error()
	}
	if isBinary(data) {
		return "", fmt.Sprintf("binary (%d bytes)", len(data))
	}
	return string(data), ""
}

// isBinary reports whether data looks non-textual: a null byte or invalid UTF-8
// is enough to skip the line diff instead of emitting garbage.
func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
}

// sameContent reports whether two files hold identical bytes, streaming them so
// an oversized file is compared without being held in memory.
func sameContent(a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()

	bufA := make([]byte, 32*1024)
	bufB := make([]byte, 32*1024)
	for {
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		switch {
		case isEOF(errA) && isEOF(errB):
			return true, nil
		case isEOF(errA) != isEOF(errB):
			return false, nil
		case errA != nil:
			return false, errA
		case errB != nil:
			return false, errB
		}
	}
}

func isEOF(err error) bool {
	return err == io.EOF || err == io.ErrUnexpectedEOF
}
