//go:build unix

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func checkSupported() error { return nil }

// prepareCmd puts the child in its own process group, so signals reach its whole tree.
func prepareCmd(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// signalGroup sends SIGKILL (kill) or SIGTERM to the process group of pid. A group that is gone is no error.
func signalGroup(pid int, kill bool) error {
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(-pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// signaledReason describes a process that was ended by a signal.
func signaledReason(st *os.ProcessState) (string, bool) {
	if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return fmt.Sprintf("signal: %s", ws.Signal()), true
	}
	return "", false
}
