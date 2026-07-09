package execjsonctl

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
)

type processMeta struct {
	Version     int       `json:"version"`
	Turn        int       `json:"turn"`
	PID         int       `json:"pid"`
	PGID        int       `json:"pgid"`
	StartedAt   time.Time `json:"started_at"`
	CWD         string    `json:"cwd"`
	Command     string    `json:"command"`
	Fingerprint string    `json:"fingerprint"`
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func signalGroup(pgid int, sig syscall.Signal) {
	if pgid > 0 {
		_ = syscall.Kill(-pgid, sig)
	}
}

// spawnTurn launches one detached `codex exec` process. It must outlive the
// agentmux CLI invocation that created it, so it gets its own session and
// process group; the group is also what halt/interrupt signal, because the npm
// `codex` entrypoint is a node shim that forks the real binary.
func spawnTurn(scriptPath, cwd string, env map[string]string) (pid, pgid int, err error) {
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Dir = cwd
	cmd.Env = envList(env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, 0, apperr.Wrap("execjson_process_error", err, "start codex exec turn")
	}
	go func() { _ = cmd.Wait() }()
	pid = cmd.Process.Pid
	pgid, _ = syscall.Getpgid(pid)
	if pgid <= 0 {
		pgid = pid
	}
	return pid, pgid, nil
}

func saveProcessMeta(path string, meta processMeta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return apperr.Wrap("execjson_process_error", err, "marshal process meta")
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return apperr.Wrap("execjson_process_error", err, "write process meta")
	}
	return nil
}

func appendCommandLog(path, line string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(time.Now().UTC().Format(time.RFC3339) + " " + line + "\n")
}

// waitForExit blocks until the pid disappears or the deadline passes.
func waitForExit(ctx context.Context, pid int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for processAlive(pid) && time.Now().Before(deadline) {
		if err := sleepPoll(ctx, 50*time.Millisecond); err != nil {
			return
		}
	}
}
