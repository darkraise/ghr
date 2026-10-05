//go:build unix

package system

import (
	"os/exec"
	"syscall"
)

// killGroup starts cmd in a process group of its own and makes cancelling it
// kill that whole group.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
