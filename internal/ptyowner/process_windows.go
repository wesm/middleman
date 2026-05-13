//go:build windows

package ptyowner

import (
	"os"
	"os/exec"
	"strconv"

	gopty "github.com/aymanbagabas/go-pty"
)

func configureOwnerCommand(*gopty.Cmd) {}

func killOwnerProcess(process *os.Process) {
	err := exec.Command(
		"taskkill", "/T", "/F", "/PID", strconv.Itoa(process.Pid),
	).Run()
	if err != nil {
		_ = process.Kill()
	}
}
