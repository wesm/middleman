//go:build !windows

package localruntime

import (
	"os"
	"syscall"
)

func killSessionProcess(process *os.Process) error {
	// pty.StartWithSize sets Setsid, so the launched process is a
	// session/pgid leader. Send SIGKILL to -pid to reach every descendant
	// in the group; otherwise detached children would outlive the session.
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err != nil {
		return process.Kill()
	}
	return nil
}
