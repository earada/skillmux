package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"tilde with subpath", "~/.claude/skills", filepath.Join(home, ".claude", "skills")},
		{"bare tilde", "~", home},
		{"absolute path unchanged", "/tmp/skills", "/tmp/skills"},
		{"relative path unchanged", "skills/foo", "skills/foo"},
		{"tilde mid-path not expanded", "/opt/~/skills", "/opt/~/skills"},
		{"tilde-prefixed name not expanded", "~user/skills", "~user/skills"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandHome(tc.in); got != tc.want {
				t.Errorf("ExpandHome(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
