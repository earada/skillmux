// Command skillmux is a TUI for managing AI-agent Skills across the Targets on
// a machine. This entrypoint is a placeholder until the TUI lands; for now it
// reports where the Config and Manifest live.
package main

import (
	"fmt"
	"os"

	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/paths"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "skillmux:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath, err := paths.ConfigFile()
	if err != nil {
		return err
	}
	manifestPath, err := paths.ManifestFile()
	if err != nil {
		return err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	man, err := manifest.Load(manifestPath)
	if err != nil {
		return err
	}

	fmt.Printf("skillmux (TUI pending)\n")
	fmt.Printf("config:   %s (%d targets, %d sources)\n", configPath, len(cfg.Targets), len(cfg.Sources))
	fmt.Printf("manifest: %s (%d installations)\n", manifestPath, len(man.Installations))
	return nil
}
