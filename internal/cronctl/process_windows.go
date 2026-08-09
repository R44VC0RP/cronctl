//go:build windows

package cronctl

import (
	"os/exec"
)

func configureProcess(_ *exec.Cmd) {}

func terminateProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", processID(cmd), "/T", "/F").Run()
}
