package providers

import (
	"context"
	"sync"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/cache"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/config"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
)

// Build returns the enabled providers in a stable display order: Claude,
// Codex, z.ai, then custom providers in config order.
func Build(cfg config.Config) []quota.Provider {
	var list []quota.Provider
	if cfg.ClaudeEnabled {
		list = append(list, NewClaude(cfg))
	}
	if cfg.CodexEnabled {
		list = append(list, NewCodex(cfg))
	}
	if cfg.ZaiEnabled && cfg.ZaiToken != "" {
		list = append(list, NewZai(cfg))
	}
	for _, c := range cfg.CustomProviders {
		list = append(list, NewCustom(c, cfg))
	}
	return list
}

// FetchAll runs every provider concurrently and returns snapshots in the same
// order as `list`. Each provider's actual upstream call is throttled by
// `cache` (see internal/cache): back-to-back invocations of this CLI reuse
// the last result instead of re-querying every time.
func FetchAll(ctx context.Context, list []quota.Provider) []quota.Snapshot {
	c := cache.New("")
	results := make([]quota.Snapshot, len(list))
	var wg sync.WaitGroup
	for i, p := range list {
		wg.Add(1)
		go func(i int, p quota.Provider) {
			defer wg.Done()
			results[i] = c.Fetch(p.ID(), func() quota.Snapshot { return p.Fetch(ctx) })
		}(i, p)
	}
	wg.Wait()
	return results
}
