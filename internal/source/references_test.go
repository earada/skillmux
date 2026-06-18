package source

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeFile creates dir/name with the given content, making parents as needed.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func refsOf(t *testing.T, body string, known ...string) []string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", body)
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	got, err := References(dir, set)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	return got
}

func TestReferencesDetectsInvocationToken(t *testing.T) {
	body := "Run a /grilling session, using the /domain-modeling skill.\n"
	got := refsOf(t, body, "grilling", "domain-modeling", "tdd")
	want := []string{"domain-modeling", "grilling"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReferencesDetectsCrossPath(t *testing.T) {
	body := "See [format](../grill-with-docs/CONTEXT-FORMAT.md) for details.\n"
	got := refsOf(t, body, "grill-with-docs", "grilling")
	want := []string{"grill-with-docs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReferencesIgnoresUnknownNames(t *testing.T) {
	// /whatever names no known Skill, so it is not a reference.
	got := refsOf(t, "Try /whatever and /grilling.\n", "grilling")
	want := []string{"grilling"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReferencesRejectsURLsAndPaths(t *testing.T) {
	cases := []string{
		"Connect to https://mcp.example.com/grilling for the API.\n", // URL
		"Open src/grilling/main.go in the editor.\n",                 // path component
		"The file foo/grilling exists.\n",                            // path component
	}
	for _, body := range cases {
		if got := refsOf(t, body, "grilling"); len(got) != 0 {
			t.Errorf("body %q: got %v, want none", body, got)
		}
	}
}

func TestReferencesWordBoundary(t *testing.T) {
	// /grilling-foo is a different token; it must not match /grilling.
	if got := refsOf(t, "Run /grilling-foo now.\n", "grilling"); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
	// A trailing non-identifier char ends the token cleanly.
	if got := refsOf(t, "Run `/grilling`.\n", "grilling"); !reflect.DeepEqual(got, []string{"grilling"}) {
		t.Errorf("backtick-wrapped: got %v, want [grilling]", got)
	}
}

func TestReferencesScansAllFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", "Read UI.md.\n")
	writeFile(t, dir, "UI.md", "Then run /prototype to explore.\n")
	writeFile(t, dir, "references/notes.md", "Also /tdd helps.\n")
	got, err := References(dir, map[string]bool{"prototype": true, "tdd": true})
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	want := []string{"prototype", "tdd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReferencesSkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", "Run /grilling.\n")
	// A binary blob that happens to contain the bytes "/tdd" must be skipped.
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("\x00\x01/tdd\x00\xff"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := References(dir, map[string]bool{"grilling": true, "tdd": true})
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	want := []string{"grilling"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReferencesDedupesAcrossOccurrences(t *testing.T) {
	body := "/grilling here, /grilling again, and ../grilling/x.md too.\n"
	got := refsOf(t, body, "grilling")
	want := []string{"grilling"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReferencesInvocationAtStartOfText(t *testing.T) {
	// The very first character being the slash must still match.
	got := refsOf(t, "/grilling at the top.\n", "grilling")
	if !reflect.DeepEqual(got, []string{"grilling"}) {
		t.Fatalf("got %v, want [grilling]", got)
	}
}
