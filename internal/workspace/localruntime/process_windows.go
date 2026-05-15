//go:build windows

package localruntime

import "os"

func killSessionProcess(process *os.Process) {
	_ = process.Kill()
}
