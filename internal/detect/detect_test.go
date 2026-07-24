package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/earada/skillmux/internal/domain"
)

// fakeHome points $HOME at a temp dir and creates the given tool roots in it.
func fakeHome(t *testing.T, roots ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, r := range roots {
		if err := os.MkdirAll(filepath.Join(home, r), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestCandidatesFindsInstalledTools(t *testing.T) {
	fakeHome(t, ".claude", ".codex")

	got := Candidates(nil)
	want := []Candidate{
		{Name: "claude-code", Path: "~/.claude/skills"},
		{Name: "codex", Path: "~/.codex/skills"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCandidatesSkipsAbsentTools(t *testing.T) {
	fakeHome(t) // no tool roots at all
	if got := Candidates(nil); len(got) != 0 {
		t.Errorf("expected no candidates on a bare machine, got %+v", got)
	}
}

func TestCandidatesSkipsToolConfiguredByName(t *testing.T) {
	fakeHome(t, ".claude")
	got := Candidates([]domain.Target{{Name: "claude-code", Path: "/elsewhere"}})
	if len(got) != 0 {
		t.Errorf("target with same name should suppress the candidate, got %+v", got)
	}
}

func TestCandidatesSkipsToolConfiguredByPath(t *testing.T) {
	fakeHome(t, ".claude")
	got := Candidates([]domain.Target{{Name: "my-claude", Path: "~/.claude/skills"}})
	if len(got) != 0 {
		t.Errorf("target with same path should suppress the candidate, got %+v", got)
	}
}

func TestCandidatesIgnoresFileAtRoot(t *testing.T) {
	home := fakeHome(t)
	if err := os.WriteFile(filepath.Join(home, ".claude"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Candidates(nil); len(got) != 0 {
		t.Errorf("a plain file is not a tool installation, got %+v", got)
	}
}
