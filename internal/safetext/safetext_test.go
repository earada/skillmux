package safetext

import (
	"strings"
	"testing"
)

func TestLineStripsControlAndEscapeSequences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"esc", "a\x1bb", "ab"},
		{"lone escape", "\x1b", ""},
		{"csi color", "\x1b[31mred\x1b[0m", "[31mred[0m"},
		{"osc52 clipboard bel", "x\x1b]52;c;aGVsbG8=\x07y", "x]52;c;aGVsbG8=y"},
		{"osc title st", "\x1b]0;pwned\x1b\\done", "]0;pwned\\done"}, // ESCs gone; backslash is inert
		{"carriage return", "safe\rEVIL", "safeEVIL"},
		{"crlf and tab", "a\r\n\tb", "ab"},
		{"bell backspace", "a\x07\x08b", "ab"},
		{"del", "a\x7fb", "ab"},
		{"c1 control utf8", "ab", "ab"},         // C1 CSI as valid UTF-8 (0xC2 0x9B)
		{"c1 control raw byte", "a\x9bb", "ab"}, // stray 8-bit C1 byte, invalid UTF-8
		{"bidi rlo", "file‮gnp.exe", "filegnp.exe"},
		{"bidi pop/isolates", "⁦a⁩‏b", "ab"},
		{"zero-width divider sentinel", "a​b", "ab"},
		{"plain unicode kept", "café — αβγ 日本語", "café — αβγ 日本語"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Line(tc.in); got != tc.want {
				t.Errorf("Line(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got := Line(tc.in); strings.ContainsRune(got, 0x1b) {
				t.Errorf("Line(%q) leaked an ESC byte: %q", tc.in, got)
			}
		})
	}
}

func TestMultilineKeepsNewlinesAndTabs(t *testing.T) {
	in := "line1\n\tindented\r\n\x1b[1mbold\x1b[0m\nend"
	want := "line1\n\tindented\n[1mbold[0m\nend" // ESCs stripped; SGR params left inert
	if got := Multiline(in); got != want {
		t.Errorf("Multiline = %q, want %q", got, want)
	}
}

func TestMultilineStripsOSC52InRawBody(t *testing.T) {
	// A "text" file whose bytes smuggle an OSC 52 clipboard write.
	in := "hello\n\x1b]52;c;cGF5bG9hZA==\x07\nworld"
	got := Multiline(in)
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) {
		t.Fatalf("multiline body still carries an escape/BEL byte: %q", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatalf("multiline body dropped visible text: %q", got)
	}
}
