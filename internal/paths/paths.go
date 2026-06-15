// Package paths resolves the XDG locations Skillmux reads and writes.
//
// Skillmux honours the XDG Base Directory spec explicitly (rather than Go's
// os.UserConfigDir, which on macOS points at ~/Library/Application Support) so
// that Config and Manifest live under ~/.config and ~/.local/state as agreed.
package paths

import (
	"os"
	"path/filepath"
)

const appName = "skillmux"

// ConfigFile is the path to the user-owned Config (TOML).
func ConfigFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// ManifestFile is the path to the Skillmux-owned Manifest (JSON).
func ManifestFile() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "manifest.json"), nil
}

// CacheDir is where fetched Source contents are cached.
func CacheDir() (string, error) {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", appName), nil
}

func configDir() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appName), nil
}

func stateDir() (string, error) {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", appName), nil
}
