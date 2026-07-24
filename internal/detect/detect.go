// Package detect finds AI coding tools installed on this machine that are not
// yet configured as Targets, so the config screen can offer them for one-key
// adoption instead of asking the user to type paths by hand (skillmux-l7f).
package detect

import (
	"os"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/paths"
)

// Candidate is a known tool present on this machine but not yet configured as
// a Target: a ready-to-add {name, path} proposal. Path is kept ~-relative so
// the Config stays portable; expansion happens where paths are consumed.
type Candidate struct {
	Name string // proposed Target name, e.g. "claude-code"
	Path string // proposed skills path, e.g. "~/.claude/skills"
}

// knownTool describes how to recognise one AI coding tool: the tool counts as
// installed when its root directory exists. The skills directory itself may
// not exist yet — installing a Skill creates it — so presence of the root is
// the signal, not presence of the skills folder.
type knownTool struct {
	name   string // proposed Target name
	root   string // directory whose existence marks the tool as installed
	skills string // skills directory proposed as the Target path
}

var knownTools = []knownTool{
	{name: "claude-code", root: "~/.claude", skills: "~/.claude/skills"},
	{name: "codex", root: "~/.codex", skills: "~/.codex/skills"},
	{name: "cursor", root: "~/.cursor", skills: "~/.cursor/skills"},
	{name: "gemini", root: "~/.gemini", skills: "~/.gemini/skills"},
	{name: "opencode", root: "~/.config/opencode", skills: "~/.config/opencode/skill"},
}

// Candidates returns the known tools installed on this machine that no
// configured Target already covers. A Target covers a tool when it has the
// same name or the same (expanded) path — either means the user has already
// made their own call about that tool, so we stop proposing it.
func Candidates(configured []domain.Target) []Candidate {
	var out []Candidate
	for _, k := range knownTools {
		if covered(k, configured) {
			continue
		}
		if info, err := os.Stat(paths.ExpandHome(k.root)); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, Candidate{Name: k.name, Path: k.skills})
	}
	return out
}

func covered(k knownTool, configured []domain.Target) bool {
	skills := paths.ExpandHome(k.skills)
	for _, t := range configured {
		if t.Name == k.name || paths.ExpandHome(t.Path) == skills {
			return true
		}
	}
	return false
}
