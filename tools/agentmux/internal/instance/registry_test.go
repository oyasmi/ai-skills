package instance

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestWithLockedPreservesConcurrentUpdates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "instances.json")
	const workers = 12

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithLocked(path, func(reg *Registry) error {
				reg.Put(Instance{Name: "worker-" + strconv.Itoa(i)})
				return nil
			})
			if err != nil {
				t.Errorf("WithLocked: %v", err)
			}
		}()
	}
	wg.Wait()

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.Instances) != workers {
		t.Fatalf("expected %d instances, got %d", workers, len(reg.Instances))
	}
}

func TestWithLockedCreatesPrivateRegistryAndLockFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "instances.json")
	if err := WithLocked(path, func(reg *Registry) error {
		reg.Put(Instance{Name: "worker"})
		return nil
	}); err != nil {
		t.Fatalf("WithLocked: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat registry: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected registry mode 0600, got %#o", got)
	}

	lockInfo, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	if got := lockInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected lock mode 0600, got %#o", got)
	}
}
