package source

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"unicode/utf8"
)

// maxRefFileSize caps how much of a file we read when hunting for references. A
// Skill's files are small prose/markdown; anything larger is almost certainly
// not an invocation site, and reading it whole would be wasteful.
const maxRefFileSize = 1 << 20 // 1 MiB

// invocationRe matches a `/<name>` invocation token: a slash immediately
// followed by a Skill-name-shaped identifier, where the character before the
// slash is neither part of a word, a path, nor a URL scheme. The leading class
// excludes word chars, `.`, `/`, `:` and `-`, so `src/grilling`, `https://h/grilling`,
// `../grilling` and `a-/grilling` do not match — only true invocation sites like
// `/grilling`, `` `/grilling` `` or one at the start of a line. The captured name
// runs to the next non-identifier char, so `/grilling-foo` captures the whole
// token and will not be mistaken for `/grilling`.
var invocationRe = regexp.MustCompile(`(?:^|[^\w./:-])/([a-zA-Z0-9][a-zA-Z0-9-]*)`)

// crossPathRe matches a `../<name>/` relative path that crosses out of the
// Skill's own folder into a sibling Skill folder — a hard reference to that
// sibling's files (e.g. `../grill-with-docs/CONTEXT-FORMAT.md`). The trailing
// slash confirms the segment names a directory, not a file.
var crossPathRe = regexp.MustCompile(`\.\./([a-zA-Z0-9][a-zA-Z0-9-]*)/`)

// References scans every text file under dir and returns, sorted and
// de-duplicated, the names in known that the Skill references — either as a
// `/<name>` invocation token or via a `../<name>/` path crossing into that
// sibling's folder. Only names present in known are returned, so a bare `/word`
// that names no Skill is ignored; this is the exact-match-against-the-catalogue
// rule that keeps loose prose from producing false dependencies. Binary files
// are skipped. Resolving these names to concrete edges (and dropping a Skill's
// reference to itself) is the caller's job, since only the caller knows which
// Skill owns dir.
func References(dir string, known map[string]bool) ([]string, error) {
	hits := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size := info.Size()
		if size == 0 || size > maxRefFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if isBinary(data) {
			return nil
		}
		collectRefs(data, known, hits)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(hits))
	for name := range hits {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// collectRefs records into hits every known name referenced in data via either
// pattern.
func collectRefs(data []byte, known map[string]bool, hits map[string]bool) {
	for _, re := range []*regexp.Regexp{invocationRe, crossPathRe} {
		for _, m := range re.FindAllSubmatch(data, -1) {
			name := string(m[1])
			if known[name] {
				hits[name] = true
			}
		}
	}
}

// isBinary reports whether data looks like a binary blob rather than text: it
// holds a NUL byte or is not valid UTF-8 in its leading window.
func isBinary(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	return !utf8.Valid(head)
}
