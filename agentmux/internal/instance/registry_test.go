package instance

import (
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
