package agent

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// KillOrphans kills instances of an earlier agent run. The agent cannot re-attach to them (stdin, console
// and exit status are gone), so it ends them and lets the controller's desired state start them again.
// A pid file counts only when the process still carries GRUS_INSTANCE=<id> in its environment, so a reused
// pid is never killed by mistake.
func KillOrphans(dir string, log *slog.Logger) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	killed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), pidFile)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		_ = os.Remove(path)
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || pid <= 1 || !carriesInstance(pid, e.Name()) {
			continue
		}
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		log.Info("killed instance process left over from an earlier agent run", "instance", e.Name(), "pid", pid)
		killed++
	}
	return killed
}

func carriesInstance(pid int, id string) bool {
	env, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return false
	}
	return bytes.Contains(append([]byte{0}, env...), []byte("\x00GRUS_INSTANCE="+id+"\x00"))
}
