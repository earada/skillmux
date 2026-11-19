package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const handEdited = `# skillmux config — edited by hand, keep these notes!
# second preamble line

# my laptop's Claude
[[target]]
name = "claude-code"
path = "~/.claude/skills" # the default location

# work machine
[[target]]
name = "work"
path = "~/work/skills"

[[source]]
name = "my-skills"
location = "https://github.com/owner/repo"
branch  = "main"     # optional; a branch, tag, or commit SHA
subpath = "skills"   # optional; narrows where skills are scanned
future_key = "kept"  # a key skillmux does not know yet

# local checkout
[[source]]
name = "local"
location = "~/dev/skills"

# freeze deploy
[[pin]]
skill = "deploy"     # freeze this skill at what is installed
source = "my-skills"

[ui]
# a table skillmux does not know about
theme = "dark"
`

// writeAndLoad writes text to a fresh config.toml and loads it back.
func writeAndLoad(t *testing.T, text string) (string, *Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return path, c
}

// saveAndRead saves c to path and returns the written text, checking that it
// loads back as exactly c.
func saveAndRead(t *testing.T, path string, c *Config) string {
	t.Helper()
	if err := Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v\n%s", err, data)
	}
	if !configsEqual(got, c) {
		t.Fatalf("saved config loads differently:\n got %+v\nwant %+v\n%s", got, c, data)
	}
	return string(data)
}

func mustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("saved file lost %q:\n%s", w, text)
		}
	}
}

func mustNotContain(t *testing.T, text string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(text, n) {
			t.Errorf("saved file still has %q:\n%s", n, text)
		}
	}
}

var untouchedNotes = []string{
	"# skillmux config — edited by hand, keep these notes!",
	"# second preamble line",
	`path = "~/.claude/skills" # the default location`,
	"# my laptop's Claude",
	`branch  = "main"     # optional; a branch, tag, or commit SHA`,
	`future_key = "kept"  # a key skillmux does not know yet`,
	"# local checkout",
	"[ui]",
	"# a table skillmux does not know about",
	`theme = "dark"`,
}

func TestSaveKeepsUntouchedCommentsWhenAddingTarget(t *testing.T) {
	path, c := writeAndLoad(t, handEdited)
	c.Targets = append(c.Targets, TargetEntry{Name: "cursor", Path: "~/.cursor/skills"})
	text := saveAndRead(t, path, c)
	mustContain(t, text, untouchedNotes...)
	mustContain(t, text, "# work machine", "# freeze deploy", "cursor")
}

func TestSaveDropsRemovedEntryWithItsComment(t *testing.T) {
	path, c := writeAndLoad(t, handEdited)
	c.Targets = c.Targets[:1] // drop "work"
	text := saveAndRead(t, path, c)
	mustContain(t, text, untouchedNotes...)
	mustNotContain(t, text, "# work machine", "~/work/skills")
}

func TestSaveRewritesEditedEntry(t *testing.T) {
	path, c := writeAndLoad(t, handEdited)
	c.Sources[1].Location = "~/elsewhere/skills"
	text := saveAndRead(t, path, c)
	mustContain(t, text, untouchedNotes[:6]...)
	mustContain(t, text, "~/elsewhere/skills", "[ui]", `theme = "dark"`)
	mustNotContain(t, text, "~/dev/skills")
}

func TestSaveKeepsCommentsAcrossPinToggle(t *testing.T) {
	path, c := writeAndLoad(t, handEdited)
	c.AddPin("lint", "local")
	text := saveAndRead(t, path, c)
	mustContain(t, text, untouchedNotes...)
	mustContain(t, text, "# freeze deploy", "freeze this skill at what is installed", "lint")

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.RemovePin("lint", "local")
	text = saveAndRead(t, path, c)
	mustContain(t, text, untouchedNotes...)
	mustContain(t, text, "# freeze deploy")
	mustNotContain(t, text, "lint")

	c.RemovePin("deploy", "my-skills")
	text = saveAndRead(t, path, c)
	mustContain(t, text, untouchedNotes...)
	mustNotContain(t, text, "# freeze deploy", "[[pin]]")
}

func TestSaveUnchangedConfigIsByteIdentical(t *testing.T) {
	path, c := writeAndLoad(t, handEdited)
	if text := saveAndRead(t, path, c); text != handEdited {
		t.Errorf("unchanged save rewrote the file:\n%s", text)
	}
}

func TestSaveTrickyFileStillRoundTrips(t *testing.T) {
	tricky := `# tricky
[[target]]
name = "a"
path = "/a"
notes = """
[[target]]
name = "ghost"
path = "/ghost"
"""

[[source]]
name = "s"
location = "/s"
list = [
  "x",
]
`
	path, c := writeAndLoad(t, tricky)
	if len(c.Targets) != 1 {
		t.Fatalf("precondition: want 1 target, got %+v", c.Targets)
	}
	c.Targets = append(c.Targets, TargetEntry{Name: "b", Path: "/b"})
	saveAndRead(t, path, c)

	c.Targets = c.Targets[1:] // drop "a", the one holding the fake header
	text := saveAndRead(t, path, c)
	mustNotContain(t, text, "ghost")
}

func TestSaveFallsBackWhenPreservedTextWouldDiffer(t *testing.T) {
	// A preamble that redefines our array makes any splice load differently;
	// the safety net must fall back to a plain marshal.
	existing := []byte("target = [] # broken on purpose\n[[source]]\nname = \"s\"\nlocation = \"/s\"\n")
	c := &Config{Sources: []SourceEntry{{Name: "s", Location: "/s"}}, Targets: []TargetEntry{{Name: "t", Path: "/t"}}}
	out, err := preserveMarshal(existing, c)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := marshalPlain(c)
	if string(out) != string(plain) {
		t.Errorf("want plain marshal fallback, got:\n%s", out)
	}
}

func TestSaveKeepsCompactLayout(t *testing.T) {
	compact := "# head\n[[target]]\nname = \"a\"\npath = \"/a\"\n[[target]]\nname = \"b\"\npath = \"/b\"\n"
	path, c := writeAndLoad(t, compact)
	if text := saveAndRead(t, path, c); text != compact {
		t.Errorf("unchanged compact file rewritten:\n%s", text)
	}
}

func TestSaveFirstSaveWithoutFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "config.toml")
	c := &Config{Targets: []TargetEntry{{Name: "t", Path: "/t"}}}
	saveAndRead(t, path, c)
}
