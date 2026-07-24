// Command skillmux is a TUI for managing AI-agent Skills across the Targets on
// a machine. With a subcommand (status / check / apply) it runs headless
// instead, for scripts, cron, and CI.
package main

import (
	"fmt"
	"os"

	"github.com/earada/skillmux/internal/cli"
	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/tui"
)

func main() {
	e, err := engine.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "skillmux:", err)
		os.Exit(2)
	}
	if len(os.Args) > 1 {
		os.Exit(cli.Run(e, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	if err := tui.Run(e); err != nil {
		fmt.Fprintln(os.Stderr, "skillmux:", err)
		os.Exit(1)
	}
}
