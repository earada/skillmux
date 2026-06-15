package manifest

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/earada/skillmux/internal/domain"
)

func TestLoadMissingFileYieldsEmptyManifest(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load of missing file: %v", err)
	}
	if len(m.Installations) != 0 {
		t.Fatalf("expected empty manifest, got %+v", m)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "manifest.json")
	m := &Manifest{}
	m.Put(domain.Installation{
		SkillName:   "deploy",
		TargetName:  "claude-code",
		SourceName:  "remote",
		Fingerprint: "abc123",
		InstalledAt: time.Unix(1700000000, 0).UTC(),
	})
	if err := Save(path, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	in, ok := got.Find("claude-code", "deploy")
	if !ok {
		t.Fatal("installation not found after round-trip")
	}
	if in.Fingerprint != "abc123" || in.SourceName != "remote" {
		t.Errorf("round-trip mismatch: %+v", in)
	}
	if got.Version != currentVersion {
		t.Errorf("version: got %d, want %d", got.Version, currentVersion)
	}
}

func TestPutReplacesSamePair(t *testing.T) {
	m := &Manifest{}
	m.Put(domain.Installation{SkillName: "deploy", TargetName: "cc", Fingerprint: "v1"})
	m.Put(domain.Installation{SkillName: "deploy", TargetName: "cc", Fingerprint: "v2"})
	if len(m.Installations) != 1 {
		t.Fatalf("expected 1 installation after replace, got %d", len(m.Installations))
	}
	if in, _ := m.Find("cc", "deploy"); in.Fingerprint != "v2" {
		t.Errorf("expected fingerprint v2, got %q", in.Fingerprint)
	}
}

func TestRemove(t *testing.T) {
	m := &Manifest{}
	m.Put(domain.Installation{SkillName: "deploy", TargetName: "cc"})
	if !m.Remove("cc", "deploy") {
		t.Error("Remove reported nothing removed")
	}
	if m.Remove("cc", "deploy") {
		t.Error("second Remove should report nothing removed")
	}
	if len(m.Installations) != 0 {
		t.Errorf("expected empty after remove, got %d", len(m.Installations))
	}
}
