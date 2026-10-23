//go:build !windows

package runner

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
