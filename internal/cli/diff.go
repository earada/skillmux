package cli

import (
	"fmt"
	"io"

	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/safetext"
)

// runDiff prints what a reinstall would change, in unified-diff form, for every
// Installation whose copy differs from its Source — the headless counterpart of
// the TUI's diff screen (skillmux-4z5), so `skillmux check` can be followed by
// "and what exactly?" without opening the TUI.
//
// With no arguments it walks every Installation, skipping the up-to-date ones
// (their diff is empty by definition). A skill — optionally with a target —
// narrows it to that pair, reports "no differences" explicitly rather than
// exiting silently (a quiet exit is indistinguishable from a bad filter), and
// falls back to an untracked folder at the destination when no Installation
// matches: that folder is still the question the user asked about.
func runDiff(e *engine.Engine, args []string, stdout, stderr io.Writer) int {
	var skill, target string
	switch len(args) {
	case 0:
	case 1:
		skill = args[0]
	case 2:
		skill, target = args[0], args[1]
	default:
		fmt.Fprintf(stderr, "skillmux: diff takes at most [skill [target]]\n\n%s", usageText)
		return exitError
	}
	explicit := skill != "" || target != ""

	cat := e.Refresh()
	// A failed Source falls back to its last cached snapshot, so the "upstream"
	// side may be stale. Say so, and let the exit code carry it.
	srcErr := reportSourceErrors(cat, stderr)

	matched, differing, failed := 0, 0, 0
	for _, r := range installedRows(e, cat) {
		if skill != "" && r.SkillName != skill {
			continue
		}
		if target != "" && r.TargetName != target {
			continue
		}
		matched++

		if r.Status == domain.StatusUnavailable {
			fmt.Fprintf(stderr, "skillmux: %s → %s: removed upstream — nothing to compare against\n",
				r.SkillName, r.TargetName)
			failed++
			continue
		}
		if r.Status == domain.StatusUpToDate && !explicit {
			continue // installed == recorded == upstream; nothing to print
		}

		sk, ok := catalogSkill(cat, r.SkillName, r.SourceName)
		if !ok {
			fmt.Fprintf(stderr, "skillmux: %s is no longer offered by source %s\n", r.SkillName, r.SourceName)
			failed++
			continue
		}
		differs, err := showDiff(e, sk, r.TargetName, explicit, stdout)
		switch {
		case err != nil:
			fmt.Fprintln(stderr, "skillmux:", err)
			failed++
		case differs:
			differing++
		}
	}

	// Nothing recorded matches, but a folder may sit at the destination anyway —
	// placed by hand or by another tool. Compare it: Skillmux would need explicit
	// confirmation to overwrite it (ADR 0002), which is worth seeing beforehand.
	if explicit && matched == 0 {
		for _, sk := range cat.Skills {
			if skill != "" && sk.Name != skill {
				continue
			}
			for _, t := range e.Config.DomainTargets() {
				if target != "" && t.Name != target {
					continue
				}
				if _, ok := e.InstalledCopy(t.Name, sk.Name); !ok {
					continue
				}
				matched++
				differs, err := showDiff(e, sk, t.Name, explicit, stdout)
				switch {
				case err != nil:
					fmt.Fprintln(stderr, "skillmux:", err)
					failed++
				case differs:
					differing++
				}
			}
		}
	}

	switch {
	case explicit && matched == 0:
		fmt.Fprintf(stderr, "skillmux: no installation matches %s\n", filterLabel(skill, target))
		return exitError
	case srcErr || failed > 0:
		return exitError
	case differing > 0:
		fmt.Fprintf(stdout, "%d installation(s) differ from their source.\n", differing)
		return exitPending
	default:
		if !explicit {
			fmt.Fprintln(stdout, "All installations match their source.")
		}
		return exitOK
	}
}

// showDiff compares one (Skill, Target) pair and writes the result, reporting
// whether anything differed. An identical pair prints a line only when the user
// asked for it by name; otherwise silence is the answer.
func showDiff(e *engine.Engine, sk engine.AvailableSkill, target string, explicit bool, stdout io.Writer) (bool, error) {
	c, err := e.Compare(sk, target)
	if err != nil {
		return false, err
	}
	if c.Summary.Empty() {
		if explicit {
			fmt.Fprintf(stdout, "%s (%s) → %s: no differences\n",
				safetext.Line(sk.Name), safetext.Line(sk.Source), safetext.Line(target))
		}
		return false, nil
	}
	writeComparison(stdout, c)
	return true, nil
}

// writeComparison prints one Comparison as a plain unified diff: the pair it is
// for, how much the old side can be trusted, the two folders, a file summary, and
// each file's hunks. Every interpolated string — paths, file names, file contents
// — is Source- or Target-controlled, so it is made inert first: this output goes
// to a terminal as readily as to a log.
func writeComparison(w io.Writer, c engine.Comparison) {
	fmt.Fprintf(w, "%s (%s) → %s\n", safetext.Line(c.SkillName), safetext.Line(c.SourceName), safetext.Line(c.TargetName))
	switch {
	case !c.Tracked:
		fmt.Fprintln(w, "! untracked — skillmux did not install this copy; overwriting it needs confirmation")
	case !c.Pristine:
		fmt.Fprintln(w, "! modified locally — the changes below mix hand-made edits with upstream's")
	}
	fmt.Fprintf(w, "--- %s\n", safetext.Line(c.InstalledDir))
	fmt.Fprintf(w, "+++ %s\n", safetext.Line(c.SourceDir))

	added, removed, modified := c.Summary.Counts()
	fmt.Fprintf(w, "%d file(s) changed: %d added, %d removed, %d modified\n",
		len(c.Summary.Changes), added, removed, modified)
	for _, ch := range c.Summary.Changes {
		fmt.Fprintf(w, "%s %s  %s\n", ch.Kind.Glyph(), safetext.Line(ch.Path), ch.Kind)
		if ch.Note != "" {
			fmt.Fprintf(w, "  (%s)\n", safetext.Line(ch.Note))
			continue
		}
		for _, h := range ch.Hunks {
			fmt.Fprintln(w, h.Header())
			for _, l := range h.Lines {
				fmt.Fprintf(w, "%s%s\n", l.Kind.Prefix(), safetext.Line(l.Text))
			}
		}
	}
	fmt.Fprintln(w)
}

// catalogSkill finds an available Skill by its (name, source) identity.
func catalogSkill(cat engine.Catalog, name, source string) (engine.AvailableSkill, bool) {
	for _, sk := range cat.Skills {
		if sk.Name == name && sk.Source == source {
			return sk, true
		}
	}
	return engine.AvailableSkill{}, false
}

// filterLabel describes the requested narrowing for an error message.
func filterLabel(skill, target string) string {
	if target == "" {
		return fmt.Sprintf("skill %q", skill)
	}
	return fmt.Sprintf("%s → %s", skill, target)
}
