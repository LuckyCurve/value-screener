//go:build windows

package runner

import (
	"os/exec"
	"syscall"
)

// hideWindow stops Windows from flashing a console window for the child.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000200, // CREATE_NEW_PROCESS_GROUP
	}
}
