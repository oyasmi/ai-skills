package ndjsonctl

import (
	"context"
	"os"
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

func usageMap(u Usage, cost float64) map[string]any {
	return map[string]any{
		"input_tokens":                u.InputTokens,
		"output_tokens":               u.OutputTokens,
		"cache_creation_input_tokens": u.CacheCreationInputTokens,
		"cache_read_input_tokens":     u.CacheReadInputTokens,
		"total_cost_usd":              cost,
	}
}
