package agent

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func startOrphan(t *testing.T, dir, id, envInstance string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	cmd.Env = []string{"GRUS_INSTANCE=" + envInstance}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id, pidFile), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestKillOrphansEndsProcessesOfAnEarlierRunOnly(t *testing.T) {
	dir := t.TempDir()
	mine := startOrphan(t, dir, "lobby-a", "lobby-a")
	reused := startOrphan(t, dir, "lobby-b", "someone-else") // pid file points at an unrelated process

	n := KillOrphans(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if n != 1 {
		t.Errorf("killed %d processes, want 1", n)
	}
	if err := mine.Wait(); err == nil {
		t.Error("the instance process survived KillOrphans")
	}
	if err := reused.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("an unrelated process was killed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "lobby-a", pidFile)); err == nil {
		t.Error("pid file was not removed")
	}
}
