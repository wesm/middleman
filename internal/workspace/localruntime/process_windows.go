//go:build windows

package localruntime

import "os"

func killSessionProcess(process *os.Process) error {
	return process.Kill()
}
