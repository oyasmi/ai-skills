package rpcctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

var zeroTime time.Time

func nowUTC() time.Time { return time.Now().UTC() }

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

// newUUID mints a RFC 4122 v4 identifier. pi's --session-id and RPC command ids
// both accept it: the value satisfies pi's session-id charset rule (alphanumerics
// and dashes, alphanumeric endpoints).
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", apperr.Wrap("rpc_process_error", err, "generate uuid")
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32]), nil
}

func usageMap(st State) map[string]any {
	return map[string]any{
		"input_tokens":       st.TotalInputTokens,
		"output_tokens":      st.TotalOutputTokens,
		"cache_read_tokens":  st.TotalCacheReadTokens,
		"cache_write_tokens": st.TotalCacheWriteTokens,
		"total_cost_usd":     st.TotalCostUSD,
	}
}
