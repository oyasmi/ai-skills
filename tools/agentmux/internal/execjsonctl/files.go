package execjsonctl

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func tailFile(path string, maxBytes int64) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if int64(len(b)) > maxBytes {
		b = b[int64(len(b))-maxBytes:]
	}
	return strings.TrimSpace(string(b))
}

func readExitCode(path string) *int {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return nil
	}
	return &code
}

func sleepPoll(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func envList(base map[string]string) []string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	for k, v := range base {
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
