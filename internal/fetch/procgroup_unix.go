//go:build unix

package fetch

import (
	"os/exec"
	"syscall"
)

// startNewProcessGroup makes cmd the leader of a fresh process group and routes
// context cancellation (the timeout) to signal the entire group. git often
// spawns children — ssh for an SSH remote, a credential helper for HTTPS — and
// killing only git would orphan a child that is the one actually blocked on a
// stalled network read or a prompt. Signalling the whole group tears the stall
// down completely so no process leaks past the deadline.
func startNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// A negative pid targets the whole process group led by cmd.Process.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
