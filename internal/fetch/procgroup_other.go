//go:build !unix

package fetch

import "os/exec"

// startNewProcessGroup is a no-op on platforms without POSIX process groups.
// Context cancellation falls back to exec.CommandContext's default, which kills
// the git process itself.
func startNewProcessGroup(cmd *exec.Cmd) {}
