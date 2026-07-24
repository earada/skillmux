// Package cli implements the non-interactive subcommands — status, check, diff,
// apply — so skillmux can run headless in dotfiles, provisioning, cron, and CI
// (skillmux-516). It drives the same Engine as the TUI; the difference is how
// desired state is derived: headless apply keeps every current Installation
// as-is, so its Plan can only contain reinstalls of drifted Installations,
// never a surprise install or uninstall.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/reconcile"
)

// Exit codes, git-style so scripts can branch on them: 0 nothing pending / all
// good; 1 updates pending (check), user declined or an operation failed
// (apply); 2 usage error or a Source failed to refresh (results untrustworthy).
const (
	exitOK      = 0
	exitPending = 1
	exitError   = 2
)

const usageText = `Usage:
  skillmux              open the TUI
  skillmux status       list installed skills and their status
  skillmux check        report pending updates; exit 1 when any are pending
  skillmux diff [skill [target]]
                        show what a reinstall would change (unified diff);
                        exit 1 when anything differs
  skillmux apply [-y]   reinstall every installation with an update available
      --yes, -y         skip the confirmation prompt
  skillmux help         show this help

Exit codes: 0 ok · 1 updates pending / something differs / declined / an
operation failed · 2 usage error or a source failed to refresh.
`

// Run dispatches one non-interactive subcommand and returns its exit code.
func Run(e *engine.Engine, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch args[0] {
	case "status":
		return runStatus(e, stdout, stderr)
	case "check":
		return runCheck(e, stdout, stderr)
	case "diff":
		return runDiff(e, args[1:], stdout, stderr)
	case "apply":
		yes := false
		for _, a := range args[1:] {
			switch a {
			case "--yes", "-y":
				yes = true
			default:
				fmt.Fprintf(stderr, "skillmux: unknown apply flag %q\n\n%s", a, usageText)
				return exitError
			}
		}
		return runApply(e, yes, stdin, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return exitOK
	default:
		fmt.Fprintf(stderr, "skillmux: unknown command %q\n\n%s", args[0], usageText)
		return exitError
	}
}

func runStatus(e *engine.Engine, stdout, stderr io.Writer) int {
	cat := e.Refresh()
	srcErr := reportSourceErrors(cat, stderr)
	rows := installedRows(e, cat)
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "No skills installed.")
	} else {
		w := tabwriter.NewWriter(stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "TARGET\tSKILL\tSOURCE\tSTATUS")
		for _, r := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.TargetName, r.SkillName, r.SourceName, r.Status)
		}
		w.Flush()
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, summarize(rows))
	}
	if srcErr {
		return exitError
	}
	return exitOK
}

func runCheck(e *engine.Engine, stdout, stderr io.Writer) int {
	cat := e.Refresh()
	srcErr := reportSourceErrors(cat, stderr)
	var pending []engine.CellStatus
	for _, r := range installedRows(e, cat) {
		switch r.Status {
		case domain.StatusUpdateAvailable:
			pending = append(pending, r)
		case domain.StatusModified:
			// Informational only: a locally modified copy needs a human decision
			// (keep the edits or discard them in the TUI), not an automated apply,
			// so it never flips the exit code to "updates pending".
			fmt.Fprintf(stdout, "modified locally: %s → %s (%s) — resolve in the TUI\n", r.SkillName, r.TargetName, r.SourceName)
		}
	}
	for _, r := range pending {
		fmt.Fprintf(stdout, "update available: %s → %s (%s)\n", r.SkillName, r.TargetName, r.SourceName)
	}
	switch {
	case srcErr:
		return exitError
	case len(pending) > 0:
		fmt.Fprintf(stdout, "%d update(s) pending — run `skillmux apply`.\n", len(pending))
		return exitPending
	default:
		fmt.Fprintln(stdout, "All installations up to date.")
		return exitOK
	}
}

func runApply(e *engine.Engine, yes bool, stdin io.Reader, stdout, stderr io.Writer) int {
	cat := e.Refresh()
	if reportSourceErrors(cat, stderr) {
		// A failed Source falls back to its last cached snapshot, so applying now
		// could "update" to stale content. Automation must not do that silently.
		fmt.Fprintln(stderr, "skillmux: refusing to apply while a source fails to refresh")
		return exitError
	}
	rows := installedRows(e, cat)
	desired := make([]reconcile.Cell, 0, len(rows))
	for _, r := range rows {
		desired = append(desired, reconcile.Cell{Skill: r.SkillName, Source: r.SourceName, Target: r.TargetName})
	}
	pre := e.Preview(desired, cat)
	// A reinstall over a locally modified copy discards hand-made edits — a
	// human decision, not one automation may take. Skip those operations with a
	// note instead of failing them, so a cron'd apply stays clean while the
	// user resolves the divergence in the TUI (skillmux-0o2).
	if len(pre.Modified) > 0 {
		skip := map[[2]string]bool{}
		for _, c := range pre.Modified {
			skip[[2]string{c.TargetName, c.SkillName}] = true
			fmt.Fprintf(stdout, "skipping %s → %s: modified locally — resolve in the TUI\n", c.SkillName, c.TargetName)
		}
		kept := pre.Plan.Operations[:0]
		for _, op := range pre.Plan.Operations {
			if !skip[[2]string{op.TargetName, op.SkillName}] {
				kept = append(kept, op)
			}
		}
		pre.Plan.Operations = kept
	}
	if len(pre.Plan.Operations) == 0 {
		fmt.Fprintln(stdout, "Nothing to do.")
		return exitOK
	}
	// Keeping installed state can only yield reinstalls of already-tracked
	// folders, which never collide (ADR 0002) — but check anyway so a future
	// semantic change can't silently start clobbering untracked folders.
	if len(pre.Collisions) > 0 {
		for _, c := range pre.Collisions {
			fmt.Fprintf(stderr, "skillmux: would overwrite untracked folder %s (%s → %s)\n", c.Dir, c.SkillName, c.TargetName)
		}
		fmt.Fprintln(stderr, "skillmux: refusing to overwrite; resolve in the TUI")
		return exitError
	}
	for _, op := range pre.Plan.Operations {
		fmt.Fprintln(stdout, describeOp(op))
	}
	if !yes {
		fmt.Fprintf(stdout, "Apply %d operation(s)? [y/N] ", len(pre.Plan.Operations))
		if !confirmed(stdin) {
			fmt.Fprintln(stdout, "Cancelled.")
			return exitPending
		}
	}
	rep, err := e.Apply(pre, apply.Options{})
	failed := 0
	for _, r := range rep.Results {
		if r.OK {
			fmt.Fprintln(stdout, "✓ "+describeOp(r.Op))
		} else {
			failed++
			fmt.Fprintf(stdout, "✗ %s  %v\n", describeOp(r.Op), r.Err)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "skillmux: persisting manifest:", err)
		return exitError
	}
	if failed > 0 {
		fmt.Fprintf(stdout, "%d ok, %d failed\n", len(rep.Results)-failed, failed)
		return exitPending
	}
	fmt.Fprintf(stdout, "%d ok\n", len(rep.Results))
	return exitOK
}

// installedRows narrows the full cell Status to actual Installations — the
// rows a headless run acts on — in a stable target/skill/source order.
func installedRows(e *engine.Engine, cat engine.Catalog) []engine.CellStatus {
	var out []engine.CellStatus
	for _, c := range e.Status(cat) {
		switch c.Status {
		case domain.StatusUpToDate, domain.StatusUpdateAvailable, domain.StatusUnavailable, domain.StatusModified:
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.TargetName != b.TargetName {
			return a.TargetName < b.TargetName
		}
		if a.SkillName != b.SkillName {
			return a.SkillName < b.SkillName
		}
		return a.SourceName < b.SourceName
	})
	return out
}

// reportSourceErrors prints each failing Source to stderr (in a stable order)
// and reports whether there were any: such a Source's rows reflect its last
// cached snapshot, not upstream.
func reportSourceErrors(cat engine.Catalog, stderr io.Writer) bool {
	names := make([]string, 0, len(cat.SourceErrors))
	for n := range cat.SourceErrors {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(stderr, "skillmux: source %s: %v\n", n, cat.SourceErrors[n])
	}
	return len(names) > 0
}

func summarize(rows []engine.CellStatus) string {
	counts := map[domain.Status]int{}
	for _, r := range rows {
		counts[r.Status]++
	}
	parts := []string{fmt.Sprintf("%d installed", len(rows))}
	if n := counts[domain.StatusUpdateAvailable]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d update(s) available", n))
	}
	if n := counts[domain.StatusUnavailable]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d unavailable upstream", n))
	}
	if n := counts[domain.StatusModified]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d modified locally", n))
	}
	return strings.Join(parts, " · ")
}

func describeOp(op reconcile.Operation) string {
	s := fmt.Sprintf("%-9s %s (%s) → %s", op.Kind, op.SkillName, op.SourceName, op.TargetName)
	if op.Reason != "" {
		s += "  [" + op.Reason + "]"
	}
	return s
}

func confirmed(stdin io.Reader) bool {
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
