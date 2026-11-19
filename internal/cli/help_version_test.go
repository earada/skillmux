package cli

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

// RunStatic must answer help / version / unknown commands without an Engine,
// so they keep working when config.toml or the manifest is broken (skillmux-au0).
func TestRunStaticHelp(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		var out, errb bytes.Buffer
		code, handled := RunStatic([]string{arg}, &out, &errb)
		if !handled || code != exitOK {
			t.Errorf("%s: code=%d handled=%v, want 0 true", arg, code, handled)
		}
		if !strings.Contains(out.String(), "Usage:") || !strings.Contains(out.String(), "skillmux version") {
			t.Errorf("%s: stdout lacks usage with version line:\n%s", arg, out.String())
		}
		if errb.Len() != 0 {
			t.Errorf("%s: unexpected stderr %q", arg, errb.String())
		}
	}
}

func TestRunStaticVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-V"} {
		var out, errb bytes.Buffer
		code, handled := RunStatic([]string{arg}, &out, &errb)
		if !handled || code != exitOK {
			t.Errorf("%s: code=%d handled=%v, want 0 true", arg, code, handled)
		}
		if !strings.HasPrefix(out.String(), "skillmux ") || !strings.HasSuffix(out.String(), "\n") ||
			strings.TrimSpace(strings.TrimPrefix(out.String(), "skillmux ")) == "" {
			t.Errorf("%s: stdout = %q, want \"skillmux <version>\\n\"", arg, out.String())
		}
	}
}

func TestRunStaticUnknown(t *testing.T) {
	var out, errb bytes.Buffer
	code, handled := RunStatic([]string{"frobnicate"}, &out, &errb)
	if !handled || code != exitError {
		t.Fatalf("code=%d handled=%v, want 2 true", code, handled)
	}
	if !strings.Contains(errb.String(), `unknown command "frobnicate"`) || !strings.Contains(errb.String(), "Usage:") {
		t.Errorf("stderr = %q", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("unexpected stdout %q", out.String())
	}
}

func TestRunStaticDefersEngineCommands(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"check"}, {"diff"}, {"diff", "x", "y"}, {"apply"}, {"apply", "--bogus"}} {
		var out, errb bytes.Buffer
		if _, handled := RunStatic(args, &out, &errb); handled {
			t.Errorf("%v: handled=true, want false", args)
		}
		if out.Len() != 0 || errb.Len() != 0 {
			t.Errorf("%v: RunStatic wrote output for an Engine command", args)
		}
	}
}

// Run keeps answering help / unknown identically to RunStatic.
func TestRunReusesStatic(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"version"}, {"nope"}} {
		var o1, e1, o2, e2 bytes.Buffer
		c1, _ := RunStatic(args, &o1, &e1)
		c2 := Run(nil, args, strings.NewReader(""), &o2, &e2)
		if c1 != c2 || o1.String() != o2.String() || e1.String() != e2.String() {
			t.Errorf("%v: Run and RunStatic diverge", args)
		}
	}
}

func TestVersionFrom(t *testing.T) {
	setting := func(kv ...string) []debug.BuildSetting {
		var s []debug.BuildSetting
		for i := 0; i < len(kv); i += 2 {
			s = append(s, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return s
	}
	cases := []struct {
		name string
		bi   *debug.BuildInfo
		ok   bool
		want string
	}{
		{"no build info", nil, false, "devel"},
		{"module version", &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, true, "v1.2.3"},
		{"devel, clean vcs", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"},
			Settings: setting("vcs.revision", "0123456789abcdef0123", "vcs.modified", "false")}, true, "0123456789ab"},
		{"devel, dirty vcs", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"},
			Settings: setting("vcs.revision", "0123456789abcdef0123", "vcs.modified", "true")}, true, "0123456789ab-dirty"},
		{"short revision", &debug.BuildInfo{Settings: setting("vcs.revision", "abc")}, true, "abc"},
		{"nothing", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "devel"},
	}
	for _, c := range cases {
		if got := versionFrom(c.bi, c.ok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
