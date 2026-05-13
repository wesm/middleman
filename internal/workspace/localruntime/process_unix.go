//go:build !windows

package localruntime

import (
	"os"
	"syscall"
)

func killSessionProcess(process *os.Process) {
	// pty.StartWithSize sets Setsid, so the launched process is a
	// session/pgid leader. Send SIGKILL to -pid to reach every
	// descendant in the group; otherwise an agent's detached children
	// would outlive the session. Fall back to single-process kill if
	// the group call fails.
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err != nil {
		_ = process.Kill()
	}
}
