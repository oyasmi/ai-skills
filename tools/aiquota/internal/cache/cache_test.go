package cache

import (
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

	c.Fetch("p", fresh)
	c.Fetch("p", fresh)
	c.Fetch("p", fresh)

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

	c.Fetch("a", fresh("a"))
	c.Fetch("b", fresh("b"))
	c.Fetch("a", fresh("a"))

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

	New(dir).Fetch("p", fresh)
	New(dir).Fetch("p", fresh)

	if calls != 1 {
		t.Fatalf("expected cache to persist across Cache instances (separate processes), got %d calls", calls)
	}
}
