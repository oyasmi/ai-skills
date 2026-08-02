// Package cache throttles upstream provider queries. aiquota is invoked as a
// fresh process on every run (often several times a minute from an agent
// loop), so an in-memory cache would not help: the throttle has to survive
// across process invocations. Each provider gets its own small JSON file
// under the cache dir, guarded by an flock so concurrent aiquota processes
// share one query per provider per MinInterval instead of each hitting the
// upstream API.
package cache

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
)

// MinInterval is the shortest gap allowed between two upstream queries for
// the same provider. Anything faster reuses the cached snapshot: repeated
// tool calls (e.g. from an agent re-checking quota every few seconds) must
// not translate 1:1 into upstream requests, or the server may flag it as abuse.
const MinInterval = 10 * time.Second

// Cache stores one entry per provider ID under Dir.
type Cache struct {
	dir string
}

// New returns a Cache rooted at dir. An empty dir falls back to DefaultDir.
func New(dir string) *Cache {
	if dir == "" {
		dir = DefaultDir()
	}
	return &Cache{dir: dir}
}

// DefaultDir is $XDG_CACHE_HOME/aiquota (or the OS equivalent), overridable
// via AIQUOTA_CACHE_DIR for tests and unusual setups.
func DefaultDir() string {
	if env := os.Getenv("AIQUOTA_CACHE_DIR"); env != "" {
		return env
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "aiquota")
	}
	return filepath.Join(os.TempDir(), "aiquota-cache")
}

type entry struct {
	FetchedAt time.Time      `json:"fetched_at"`
	Snapshot  quota.Snapshot `json:"snapshot"`
}

// Fetch returns id's snapshot, calling `fresh` only when force is set, or the
// last upstream query for id is missing or older than MinInterval; otherwise
// it returns the cached snapshot unchanged. The read-decide-write cycle holds
// an exclusive flock on id's cache file, so this also serializes concurrent
// aiquota processes racing for the same provider. Any cache I/O failure
// (unwritable dir, corrupt file, ...) falls back to calling `fresh` directly
// rather than blocking the query.
//
// A StateError snapshot is never written to the cache: a transient upstream
// hiccup (network blip, 429) must not pin every caller to the same failure
// for the rest of MinInterval, since retrying is exactly what callers (often
// an agent loop) want to be able to do.
func (c *Cache) Fetch(id string, force bool, fresh func() quota.Snapshot) quota.Snapshot {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return fresh()
	}

	f, err := os.OpenFile(c.path(id), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fresh()
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fresh()
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	var e entry
	if data, err := io.ReadAll(f); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &e)
	}

	if !force && !e.FetchedAt.IsZero() && time.Since(e.FetchedAt) < MinInterval {
		return e.Snapshot
	}

	snap := fresh()
	if snap.State == quota.StateError {
		return snap
	}
	e = entry{FetchedAt: time.Now(), Snapshot: snap}
	if data, err := json.Marshal(e); err == nil {
		if _, err := f.Seek(0, 0); err == nil {
			if err := f.Truncate(0); err == nil {
				_, _ = f.Write(data)
			}
		}
	}
	return snap
}

func (c *Cache) path(id string) string {
	return filepath.Join(c.dir, safeName(id)+".json")
}

// safeName replaces path separators in provider IDs like "custom:my-id" so
// they can't escape the cache dir or collide with it as a literal name.
func safeName(id string) string {
	out := make([]rune, 0, len(id))
	for _, r := range id {
		switch r {
		case '/', '\\', ':':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
