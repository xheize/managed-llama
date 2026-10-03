package process

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// HideConsole suppresses console windows without changing pipes, cancellation,
// or privileges. Windows still handles any separately requested UAC prompt.
func HideConsole(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	return cmd
}
