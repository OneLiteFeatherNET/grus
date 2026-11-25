//go:build !unix

package process

import (
	"errors"
	"os"
	"os/exec"
)

func checkSupported() error {
	return errors.New("process driver is not supported on this operating system")
}

func prepareCmd(*exec.Cmd) {}

func signalGroup(int, bool) error { return checkSupported() }

func signaledReason(*os.ProcessState) (string, bool) { return "", false }
