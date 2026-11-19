package config

import (
	"bytes"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	toml "github.com/pelletier/go-toml/v2"
)

// The Config file is user-owned and hand-editable, so a save from the TUI must
// not flatten it. preserveMarshal re-emits, verbatim, every block of the
// existing file whose entry is unchanged — with its comments, inline comments
// and unknown keys — and freshly marshals only the entries that are new or
// edited. It works at block granularity on lines, not on a TOML AST: anything
// it cannot handle is caught by a final round-trip check that falls back to the
// plain marshal, so a save never writes a file that loads as a different Config.

// Entry kinds, in the order toml.Marshal emits Config's fields.
const (
	kindTarget     = "target"
	kindSource     = "source"
	kindSuggestion = "suggestion"
	kindPin        = "pin"
)

// headerRe matches a table or array-of-tables header line, with an optional
// trailing comment. Group 1 is the opening bracket(s), group 2 the key.
var headerRe = regexp.MustCompile(`^\s*(\[\[?)\s*([^\[\]#]+?)\s*\]\]?\s*(#.*)?$`)

// block is one span of the existing file: the comment lines documenting a
// header, the header, and everything up to the next block's leading comments.
type block struct {
	kind  string // an entry kind, or "" for an opaque unknown table
	text  string
	index int // position among the file's blocks
}

// candidate is an existing entry block together with the entry it decodes to.
type candidate struct {
	b     block
	entry any
	used  bool
}

// marshalPlain is the historical encoding: the whole Config at once.
func marshalPlain(c *Config) ([]byte, error) { return toml.Marshal(c) }

// preserveMarshal encodes c, reusing the text of existing (the current file's
// contents) wherever its entries are unchanged. It returns the plain marshal
// whenever the spliced text would not load back as exactly c.
func preserveMarshal(existing []byte, c *Config) ([]byte, error) {
	plain, err := marshalPlain(c)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		return plain, nil
	}
	out, ok := splice(string(existing), c)
	if !ok {
		return plain, nil
	}
	var back Config
	if err := toml.Unmarshal([]byte(out), &back); err != nil || !configsEqual(&back, c) {
		return plain, nil
	}
	return []byte(out), nil
}

// splice builds the preserved text, or reports false if any step fails.
func splice(existing string, c *Config) (string, bool) {
	preamble, blocks := splitBlocks(existing)

	// Existing entry blocks by kind, each with the entry it decodes to.
	byKind := map[string][]*candidate{}
	var opaque []block
	for _, b := range blocks {
		if b.kind == "" {
			opaque = append(opaque, b)
			continue
		}
		e, ok := decodeBlock(b)
		if !ok {
			continue // unreadable alone: drop it, the entry is re-marshalled
		}
		byKind[b.kind] = append(byKind[b.kind], &candidate{b: b, entry: e})
	}

	var out strings.Builder
	out.WriteString(preamble)
	// Index of the previously emitted kept block: -1 is the preamble (which sits
	// right before block 0), freshBlock a newly marshalled entry.
	const freshBlock = -2
	prev := -1
	emit := func(text string, index int) {
		s := out.String()
		if s != "" && !strings.HasSuffix(s, "\n") {
			out.WriteString("\n")
			s += "\n"
		}
		// Keep original adjacency; otherwise separate pieces by a blank line.
		adjacent := index >= 0 && prev != freshBlock && index == prev+1
		if s != "" && !adjacent && !strings.HasSuffix(s, "\n\n") {
			out.WriteString("\n")
		}
		out.WriteString(text)
		prev = index
	}

	for _, kind := range []string{kindTarget, kindSource, kindSuggestion, kindPin} {
		entries := entriesOf(c, kind)
		// Pass 1: an entry that is unchanged keeps its block verbatim. Done for
		// the whole kind first, so an edited entry can never claim a block that
		// still matches another entry exactly.
		hits := make([]*candidate, len(entries))
		for i, e := range entries {
			for _, cand := range byKind[kind] {
				if !cand.used && reflect.DeepEqual(cand.entry, e) {
					cand.used, hits[i] = true, cand
					break
				}
			}
		}
		// Pass 2: an edited entry is paired with the block it was edited from,
		// and only its changed keys are rewritten in place — so its comments and
		// unknown keys survive the edit too (skillmux-rv6).
		patched := make([]string, len(entries))
		for i, e := range entries {
			if hits[i] != nil {
				continue
			}
			cand := editedFrom(byKind[kind], e, countNil(hits))
			if cand == nil {
				continue
			}
			if text, ok := patchBlock(cand.b, cand.entry, e); ok {
				cand.used, hits[i], patched[i] = true, cand, text
			}
		}
		for i, e := range entries {
			switch {
			case hits[i] != nil && patched[i] != "":
				emit(patched[i], hits[i].b.index)
			case hits[i] != nil:
				emit(hits[i].b.text, hits[i].b.index)
			default:
				fresh, err := marshalEntry(kind, e)
				if err != nil {
					return "", false
				}
				emit(string(fresh), freshBlock)
			}
		}
	}
	// Unknown tables go last so they cannot swallow our arrays of tables.
	for _, b := range opaque {
		emit(b.text, b.index)
	}
	s := out.String()
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s, true
}

// splitBlocks splits text into the preamble (everything before the first
// block) and its blocks. A block starts at the contiguous comment lines right
// above a header; a comment run reaching the top of the file stays preamble.
// Sub-tables of an entry kind ([target.x]) continue the current block.
func splitBlocks(text string) (string, []block) {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	type start struct {
		line int
		kind string
	}
	var starts []start
	inString := "" // the open multi-line string delimiter, if any
	curKind := ""
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		if inString != "" {
			if strings.Count(line, inString)%2 == 1 {
				inString = ""
			}
			continue
		}
		if m := headerRe.FindStringSubmatch(line); m != nil {
			key := m[2]
			first := strings.TrimSpace(strings.SplitN(key, ".", 2)[0])
			switch {
			case m[1] == "[[" && isEntryKind(key):
				curKind = key
				starts = append(starts, start{i, key})
			case curKind != "" && first == curKind && key != curKind:
				// a sub-table of the current entry: part of its block
			default:
				curKind = ""
				starts = append(starts, start{i, ""})
			}
			continue
		}
		for _, d := range []string{`"""`, `'''`} {
			if strings.Count(line, d)%2 == 1 {
				inString = d
				break
			}
		}
	}

	// Pull each header's leading comments into its block.
	cuts := make([]int, len(starts))
	for k, s := range starts {
		j := s.line
		for j > 0 && strings.HasPrefix(strings.TrimSpace(lines[j-1]), "#") {
			j--
		}
		if j == 0 {
			j = s.line // a comment run from the top of the file is preamble
		}
		if k > 0 && j <= starts[k-1].line {
			j = s.line
		}
		cuts[k] = j
	}

	if len(starts) == 0 {
		return text, nil
	}
	preamble := strings.Join(lines[:cuts[0]], "")
	blocks := make([]block, len(starts))
	for k, s := range starts {
		end := len(lines)
		if k+1 < len(starts) {
			end = cuts[k+1]
		}
		blocks[k] = block{kind: s.kind, text: strings.Join(lines[cuts[k]:end], ""), index: k}
	}
	return preamble, blocks
}

func isEntryKind(key string) bool {
	switch key {
	case kindTarget, kindSource, kindSuggestion, kindPin:
		return true
	}
	return false
}

// decodeBlock decodes a single entry block on its own, returning the entry
// value (e.g. a TargetEntry) when it holds exactly one.
func decodeBlock(b block) (any, bool) {
	var c Config
	if err := toml.Unmarshal([]byte(b.text), &c); err != nil {
		return nil, false
	}
	es := entriesOf(&c, b.kind)
	if len(es) != 1 || len(entriesOfAll(&c)) != 1 {
		return nil, false
	}
	return es[0], true
}

func entriesOf(c *Config, kind string) []any {
	var out []any
	switch kind {
	case kindTarget:
		for _, e := range c.Targets {
			out = append(out, e)
		}
	case kindSource:
		for _, e := range c.Sources {
			out = append(out, e)
		}
	case kindSuggestion:
		for _, e := range c.Suggestions {
			out = append(out, e)
		}
	case kindPin:
		for _, e := range c.Pins {
			out = append(out, e)
		}
	}
	return out
}

func entriesOfAll(c *Config) []any {
	return append(append(append(entriesOf(c, kindTarget), entriesOf(c, kindSource)...),
		entriesOf(c, kindSuggestion)...), entriesOf(c, kindPin)...)
}

// marshalEntry encodes one entry as its own [[kind]] block.
func marshalEntry(kind string, e any) ([]byte, error) {
	var one Config
	switch v := e.(type) {
	case TargetEntry:
		one.Targets = []TargetEntry{v}
	case SourceEntry:
		one.Sources = []SourceEntry{v}
	case SuggestionEntry:
		one.Suggestions = []SuggestionEntry{v}
	case PinEntry:
		one.Pins = []PinEntry{v}
	}
	data, err := toml.Marshal(one)
	if err != nil {
		return nil, err
	}
	// Targets and Sources are not omitempty, so an empty sibling may be
	// encoded as "target = []"; keep only the [[kind]] block itself.
	if i := bytes.Index(data, []byte("[["+kind+"]]")); i >= 0 {
		data = data[i:]
	}
	return data, nil
}

// configsEqual compares two Configs, treating nil and empty slices as equal.
func configsEqual(a, b *Config) bool {
	norm := func(c *Config) Config {
		n := *c
		if len(n.Targets) == 0 {
			n.Targets = nil
		}
		if len(n.Sources) == 0 {
			n.Sources = nil
		}
		if len(n.Suggestions) == 0 {
			n.Suggestions = nil
		}
		if len(n.Pins) == 0 {
			n.Pins = nil
		}
		return n
	}
	return reflect.DeepEqual(norm(a), norm(b))
}

func countNil(hits []*candidate) int {
	n := 0
	for _, h := range hits {
		if h == nil {
			n++
		}
	}
	return n
}

// editedFrom picks the unused block an edited entry most plausibly came from:
// the one carrying the same identity (its first key — a Target's or Source's
// name, a Suggestion's from, a Pin's skill), or, when the identity itself was
// edited, the only block left over if this is also the only unmatched entry —
// which is exactly what one edit in the config form produces. Anything more
// ambiguous returns nil and the entry is marshalled fresh.
func editedFrom(cands []*candidate, e any, unmatched int) *candidate {
	var free []*candidate
	for _, c := range cands {
		if !c.used {
			free = append(free, c)
		}
	}
	id := reflect.ValueOf(e).Field(0).String()
	var same []*candidate
	for _, c := range free {
		if reflect.ValueOf(c.entry).Field(0).String() == id {
			same = append(same, c)
		}
	}
	switch {
	case len(same) == 1:
		return same[0]
	case len(same) == 0 && len(free) == 1 && unmatched == 1:
		return free[0]
	}
	return nil
}

// keyLineRe matches a single-line `key = "value"` / `key = 'value'` pair with
// an optional trailing comment. Groups: indent+key+equals, the quoted value,
// the rest (spacing and comment).
var keyLineRe = regexp.MustCompile(`^(\s*([A-Za-z0-9_-]+)\s*=\s*)("(?:[^"\\]|\\.)*"|'[^']*')(\s*(?:#.*)?)$`)

// patchBlock rewrites, inside b's text, only the keys whose value differs
// between the entry the block decodes to (from) and the wanted one (to):
// a changed value is replaced on its own line (indentation and trailing comment
// kept), a value that became empty has its line dropped, and a key the block
// lacks is added after the last key line. It reports false — so the caller
// marshals the entry fresh — whenever the block is not plainly line-shaped
// (multi-line strings, a key written some other way) or the result does not
// decode to exactly `to`.
func patchBlock(b block, from, to any) (string, bool) {
	if strings.Contains(b.text, `"""`) || strings.Contains(b.text, "'''") {
		return "", false
	}
	lines := strings.SplitAfter(b.text, "\n")
	// The entry's own keys sit between its [[kind]] header and the next header.
	head, end := -1, len(lines)
	for i, raw := range lines {
		if headerRe.MatchString(strings.TrimRight(raw, "\r\n")) {
			if head == -1 {
				head = i
			} else {
				end = i
				break
			}
		}
	}
	if head == -1 {
		return "", false
	}
	keyLine := map[string]int{}
	lastKey := head
	for i := head + 1; i < end; i++ {
		if m := keyLineRe.FindStringSubmatch(strings.TrimRight(lines[i], "\r\n")); m != nil {
			keyLine[m[2]] = i
			lastKey = i
		}
	}

	fv, tv, typ := reflect.ValueOf(from), reflect.ValueOf(to), reflect.TypeOf(to)
	if fv.Type() != typ {
		return "", false
	}
	var added []string
	drop := map[int]bool{}
	for f := 0; f < typ.NumField(); f++ {
		oldVal, newVal := fv.Field(f).String(), tv.Field(f).String()
		if oldVal == newVal {
			continue
		}
		key := strings.Split(typ.Field(f).Tag.Get("toml"), ",")[0]
		i, present := keyLine[key]
		switch {
		case newVal == "":
			if !present {
				return "", false // the old value came from somewhere we cannot see
			}
			drop[i] = true
		case present:
			q, ok := quoteValue(newVal)
			if !ok {
				return "", false
			}
			raw := lines[i]
			body := strings.TrimRight(raw, "\r\n")
			m := keyLineRe.FindStringSubmatch(body)
			lines[i] = m[1] + q + m[4] + raw[len(body):]
		default:
			if oldVal != "" {
				return "", false // set, but not on a line we recognised
			}
			q, ok := quoteValue(newVal)
			if !ok {
				return "", false
			}
			added = append(added, key+" = "+q+"\n")
		}
	}

	var out strings.Builder
	for i, l := range lines {
		if !drop[i] {
			out.WriteString(l)
			if i == lastKey && !strings.HasSuffix(l, "\n") && len(added) > 0 {
				out.WriteString("\n")
			}
		}
		if i == lastKey {
			for _, a := range added {
				out.WriteString(a)
			}
		}
	}
	text := out.String()
	got, ok := decodeBlock(block{kind: b.kind, text: text})
	if !ok || !reflect.DeepEqual(got, to) {
		return "", false
	}
	return text, true
}

// quoteValue renders v as a single-line TOML string: a literal '…' string when
// it can be (matching what a fresh marshal writes), else a basic "…" string. It
// reports false for a value that cannot sit on one line.
func quoteValue(v string) (string, bool) {
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	if !strings.Contains(v, "'") {
		return "'" + v + "'", true
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`, true
}
