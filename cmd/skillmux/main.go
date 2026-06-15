// Command skillmux is a TUI for managing AI-agent Skills across the Targets on
// a machine.
package main

import (
	"fmt"
	"os"

	"github.com/earada/skillmux/internal/engine"
	"github.com/earada/skillmux/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "skillmux:", err)
		os.Exit(1)
	}
}

func run() error {
	e, err := engine.Load()
	if err != nil {
		return err
	}
	return tui.Run(e)
}
