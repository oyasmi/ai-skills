package cache

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
)

func TestFetchReusesRecentResult(t *testing.T) {
	c := New(t.TempDir())
	calls := 0
	fresh := func() quota.Snapshot {
		calls++
		return quota.Snapshot{ID: "p", State: quota.StateOK}
	}

	c.Fetch("p", false, fresh)
	c.Fetch("p", false, fresh)
	c.Fetch("p", false, fresh)

	if calls != 1 {
		t.Fatalf("expected 1 upstream call within MinInterval, got %d", calls)
	}
}

func TestFetchIsolatesProviders(t *testing.T) {
	c := New(t.TempDir())
	calls := map[string]int{}
	fresh := func(id string) func() quota.Snapshot {
		return func() quota.Snapshot {
			calls[id]++
			return quota.Snapshot{ID: id, State: quota.StateOK}
		}
	}

	c.Fetch("a", false, fresh("a"))
	c.Fetch("b", false, fresh("b"))
	c.Fetch("a", false, fresh("a"))

	if calls["a"] != 1 || calls["b"] != 1 {
		t.Fatalf("expected each provider fetched once, got %+v", calls)
	}
}

func TestFetchAcrossInstancesShareCache(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	fresh := func() quota.Snapshot {
		calls++
		return quota.Snapshot{ID: "p", State: quota.StateOK}
	}

	New(dir).Fetch("p", false, fresh)
	New(dir).Fetch("p", false, fresh)

	if calls != 1 {
		t.Fatalf("expected cache to persist across Cache instances (separate processes), got %d calls", calls)
	}
}

// A transient upstream failure must not pin every caller within MinInterval
// to the same error: retrying is exactly what an agent loop wants to do.
func TestFetchDoesNotCacheErrorState(t *testing.T) {
	c := New(t.TempDir())
	calls := 0
	fresh := func() quota.Snapshot {
		calls++
		if calls == 1 {
			return quota.Snapshot{ID: "p", State: quota.StateError, Error: "网络请求失败"}
		}
		return quota.Snapshot{ID: "p", State: quota.StateOK}
	}

	first := c.Fetch("p", false, fresh)
	second := c.Fetch("p", false, fresh)

	if first.State != quota.StateError {
		t.Fatalf("expected first call to surface the error, got %+v", first)
	}
	if second.State != quota.StateOK || calls != 2 {
		t.Fatalf("expected error snapshot to bypass the cache so the next call retries upstream, got state=%v calls=%d", second.State, calls)
	}
}

// no_data is a stable outcome (not logged in / not configured) and should
// still be cached like ok, unlike a transient error.
func TestFetchCachesNoDataState(t *testing.T) {
	c := New(t.TempDir())
	calls := 0
	fresh := func() quota.Snapshot {
		calls++
		return quota.Snapshot{ID: "p", State: quota.StateNoData}
	}

	c.Fetch("p", false, fresh)
	c.Fetch("p", false, fresh)

	if calls != 1 {
		t.Fatalf("expected no_data to be cached like ok, got %d calls", calls)
	}
}

func TestFetchForceBypassesThrottle(t *testing.T) {
	c := New(t.TempDir())
	calls := 0
	fresh := func() quota.Snapshot {
		calls++
		return quota.Snapshot{ID: "p", State: quota.StateOK}
	}

	c.Fetch("p", false, fresh)
	c.Fetch("p", true, fresh)
	c.Fetch("p", false, fresh) // still within MinInterval of the forced call

	if calls != 2 {
		t.Fatalf("expected --refresh to force exactly one extra upstream call, got %d", calls)
	}
}

func TestFetchCorruptCacheFileFallsBackToFresh(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	if err := os.WriteFile(filepath.Join(dir, "p.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := 0
	snap := c.Fetch("p", false, func() quota.Snapshot {
		calls++
		return quota.Snapshot{ID: "p", State: quota.StateOK}
	})

	if calls != 1 || snap.State != quota.StateOK {
		t.Fatalf("expected a corrupt cache file to be treated as a miss, got calls=%d snap=%+v", calls, snap)
	}
}

// Concurrent processes racing for the same provider must serialize on the
// flock and share one upstream call, never each open their own connection.
func TestFetchSerializesConcurrentCallers(t *testing.T) {
	dir := t.TempDir()
	var calls int32
	var wg sync.WaitGroup
	const n = 20
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			New(dir).Fetch("p", false, func() quota.Snapshot {
				atomic.AddInt32(&calls, 1)
				return quota.Snapshot{ID: "p", State: quota.StateOK}
			})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected concurrent callers to share one upstream call via flock, got %d", got)
	}
}
