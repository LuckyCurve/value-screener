//go:build !windows

package runner

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// prepare puts the child in a process group of its own, so its own children
// can be killed with it.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// watch kills the child's whole process group when ctx is cancelled.
//
// A frozen data layer starts the real program as a child of its own, so
// killing only the process Go started would leave the one holding the OpenD
// connection running its work to the end. The negative pid names the group
// prepare made the child the leader of.
func watch(ctx context.Context, cmd *exec.Cmd) (func(), error) {
	pid := cmd.Process.Pid
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() { close(done) })
	}
	go func() {
		select {
		case <-ctx.Done():
			// Stop the work, then let the child write what it has and
			// exit: the partial result is worth showing.
			_ = syscall.Kill(-pid, syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		case <-done:
		}
	}()
	return stop, nil
}
