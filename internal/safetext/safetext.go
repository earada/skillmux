// Package safetext makes externally-controlled text inert before it is written
// to a terminal.
//
// Skill names, descriptions, folder groups, file names, raw file contents, git
// revision labels, and Source/subprocess errors all originate outside skillmux —
// a Source repository controls them. Whether that text is interpolated into a
// Bubble Tea view or printed by a headless subcommand, it ends up written
// straight to a terminal, so a malicious repository can smuggle ANSI/OSC escape
// sequences (OSC 52 clipboard writes, a terminal-title change, cursor moves) or
// bidi overrides through an otherwise innocent-looking string. The application's
// own styling — Lip Gloss colours, Glamour markdown — is the *only* legitimate
// source of escape sequences, so every untrusted string must be made inert
// before it reaches that layer.
//
// Line/Multiline drop every byte the terminal would act on rather than display:
// the ESC introducer (which neutralises any ANSI/OSC/CSI/DCS sequence built on
// it), the rest of the C0 controls (including carriage return, a same-line
// overwrite/spoofing vector), DEL, the C1 controls, the Unicode bidirectional
// overrides, and the zero-width space skillmux itself uses as a table-divider
// sentinel (so a name can't forge a matrix rule). What remains is visible, inert
// text safe to hand to Lip Gloss, Glamour, or a plain io.Writer.
package safetext

import (
	"strings"
	"unicode/utf8"
)

// Line renders untrusted text as an inert single line: newlines and tabs are
// dropped along with every control/override rune, so the value stays on the one
// line the layout allotted it. Use it for names, descriptions, paths, labels,
// and one-line error strings.
func Line(s string) string { return clean(s, false) }

// Multiline is Line for text presented across many lines — raw file bodies, the
// markdown fed to Glamour, a printed diff. It preserves '\n' and '\t'
// (paragraphs and indentation) but is otherwise identically strict.
func Multiline(s string) string { return clean(s, true) }

func clean(s string, keepVertical bool) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	changed := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		// An invalid byte decodes as RuneError with size 1; drop it so a stray
		// 8-bit C1 byte (e.g. 0x9B, C1 CSI) can never reach an 8-bit terminal.
		if r == utf8.RuneError && size == 1 {
			changed = true
			i++
			continue
		}
		i += size
		if keepVertical && (r == '\n' || r == '\t') {
			b.WriteRune(r)
			continue
		}
		if keepRune(r) {
			b.WriteRune(r)
			continue
		}
		changed = true
	}
	if !changed {
		return s
	}
	return b.String()
}

// keepRune reports whether r is inert visible text (safe to keep). Everything
// the terminal would interpret as a command — or use to spoof — is rejected.
func keepRune(r rune) bool {
	switch {
	case r == 0x1B, r == 0x7F: // ESC (escape-sequence introducer), DEL
		return false
	case r < 0x20: // C0 controls, incl. CR/LF/TAB (callers re-admit LF/TAB)
		return false
	case r >= 0x80 && r <= 0x9F: // C1 controls
		return false
	case isBidiControl(r):
		return false
	case r == 0x200B: // zero-width space — skillmux's divider sentinel
		return false
	}
	return true
}

// isBidiControl reports whether r is a Unicode bidirectional formatting
// override. These reorder surrounding glyphs, letting untrusted text disguise
// what it actually says (e.g. hiding a "deprecated" segment or flipping a path).
func isBidiControl(r rune) bool {
	switch r {
	case 0x061C, // Arabic Letter Mark
		0x200E, 0x200F, // LRM, RLM
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // LRE, RLE, PDF, LRO, RLO
		0x2066, 0x2067, 0x2068, 0x2069: // LRI, RLI, FSI, PDI
		return true
	}
	return false
}
