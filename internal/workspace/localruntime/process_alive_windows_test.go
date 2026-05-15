//go:build windows

package localruntime

import (
	"os/exec"
	"strconv"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-Command",
		"if (Get-Process -Id "+strconv.Itoa(pid)+" -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }",
	)
	return cmd.Run() == nil
}
