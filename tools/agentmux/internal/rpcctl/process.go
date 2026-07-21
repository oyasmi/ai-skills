package rpcctl

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

type processMeta struct {
	Version     int       `json:"version"`
	PID         int       `json:"pid"`
	PGID        int       `json:"pgid"`
	StartedAt   time.Time `json:"started_at"`
	CWD         string    `json:"cwd"`
	Command     string    `json:"command"`
	Argv0       string    `json:"argv0"`
	Fingerprint string    `json:"fingerprint"`
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return nil
	}
	err := syscall.Kill(-pgid, sig)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func saveProcessMeta(path string, meta processMeta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return apperr.Wrap("rpc_process_error", err, "marshal process meta")
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return apperr.Wrap("rpc_process_error", err, "write process meta")
	}
	return nil
}

func envList(base map[string]string, extra map[string]string) []string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			env[k] = v
		}
	}
	for k, v := range base {
		env[k] = v
	}
	for k, v := range extra {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}
