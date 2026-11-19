package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const editedFile = `# my setup
# the main tool
[[target]]
name = "cc" # short name
path = "~/.claude/skills" # where skills go
future_key = 1

# team skills
[[source]]
name = "team" # ours
location = "https://github.com/o/r"
branch = "main" # pinned branch
`

func saveEdited(t *testing.T, edit func(*Config)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(editedFile), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	edit(c)
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !configsEqual(back, c) {
		t.Fatalf("saved file loads as a different Config:\n%+v\n%+v", back, c)
	}
	out, _ := os.ReadFile(path)
	return string(out)
}

func wantAll(t *testing.T, out string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
}

func TestEditedEntryKeepsItsComments(t *testing.T) {
	out := saveEdited(t, func(c *Config) { c.Targets[0].Path = "/new/path" })
	wantAll(t, out, "# the main tool", `name = "cc" # short name`, "'/new/path' # where skills go", "future_key = 1", "# team skills")
	if strings.Contains(out, ".claude/skills") {
		t.Errorf("old value survived:\n%s", out)
	}
}

func TestRenamedEntryKeepsItsComments(t *testing.T) {
	out := saveEdited(t, func(c *Config) { c.Sources[0].Name = "crew" })
	wantAll(t, out, "# team skills", "name = 'crew' # ours", `branch = "main" # pinned branch`, "# the main tool")
}

func TestEditAddsAndDropsOptionalKeys(t *testing.T) {
	out := saveEdited(t, func(c *Config) { c.Sources[0].Subpath = "skills"; c.Sources[0].Branch = "" })
	wantAll(t, out, "# team skills", `name = "team" # ours`, "subpath = 'skills'")
	if strings.Contains(out, "branch") {
		t.Errorf("emptied key survived:\n%s", out)
	}
}

func TestEditWithQuoteInValue(t *testing.T) {
	out := saveEdited(t, func(c *Config) { c.Targets[0].Path = `/it's/a "path"\x` })
	wantAll(t, out, "# the main tool", "# where skills go")
}

func TestEditDoesNotStealAnExactMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "[[target]]\nname = \"a\" # first\npath = \"/a\"\n\n[[target]]\nname = \"b\" # second\npath = \"/b\"\n"
	os.WriteFile(path, []byte(src), 0o644)
	c, _ := Load(path)
	c.Targets[0], c.Targets[1] = c.Targets[1], c.Targets[0] // reorder
	c.Targets[1].Path = "/a2"                               // and edit "a"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	wantAll(t, string(out), `name = "a" # first`, `name = "b" # second`, "'/a2'")
	back, _ := Load(path)
	if !configsEqual(back, c) {
		t.Fatalf("round trip mismatch: %+v", back)
	}
}
