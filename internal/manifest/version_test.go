package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRejectsNewerFormatVersion(t *testing.T) {
	path := writeManifest(t, `{"version": 3, "installations": []}`)
	m, err := Load(path)
	if err == nil {
		t.Fatalf("Load of version-3 manifest: expected error, got %+v", m)
	}
	msg := err.Error()
	for _, want := range []string{path, "format version 3", "up to 1", "upgrade skillmux"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
}

func TestLoadAcceptsKnownFormatVersions(t *testing.T) {
	cases := map[string]string{
		"absent (legacy)": `{"installations": [{"skill": "deploy", "target": "claude-code"}]}`,
		"zero":            `{"version": 0, "installations": []}`,
		"current":         `{"version": 1, "installations": []}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeManifest(t, body)); err != nil {
				t.Fatalf("Load: %v", err)
			}
		})
	}
}

func TestLoadMissingFileHasCurrentVersion(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Version != currentVersion {
		t.Fatalf("Version = %d, want %d", m.Version, currentVersion)
	}
}
