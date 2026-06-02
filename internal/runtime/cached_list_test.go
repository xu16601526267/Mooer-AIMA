package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type cachedListTestRuntime struct {
	name     string
	listFunc func(context.Context) ([]*DeploymentStatus, error)
}

func (r *cachedListTestRuntime) Deploy(context.Context, *DeployRequest) error { return nil }
func (r *cachedListTestRuntime) Delete(context.Context, string) error         { return nil }
func (r *cachedListTestRuntime) Status(context.Context, string) (*DeploymentStatus, error) {
	return nil, nil
}
func (r *cachedListTestRuntime) List(ctx context.Context) ([]*DeploymentStatus, error) {
	return r.listFunc(ctx)
}
func (r *cachedListTestRuntime) Logs(context.Context, string, int) (string, error) { return "", nil }
func (r *cachedListTestRuntime) Name() string                                      { return r.name }

func TestCachedListRuntimeCachesListWithinTTL(t *testing.T) {
	t.Parallel()

	var calls int32
	rt := NewCachedListRuntime(&cachedListTestRuntime{
		name: "k3s",
		listFunc: func(context.Context) ([]*DeploymentStatus, error) {
			atomic.AddInt32(&calls, 1)
			return []*DeploymentStatus{{
				Name:    "aima-vllm",
				Phase:   "running",
				Ready:   true,
				Labels:  map[string]string{"aima.dev/engine": "vllm"},
				Config:  map[string]any{"port": 32100},
				Runtime: "k3s",
			}}, nil
		},
	}, time.Hour)

	first, err := rt.List(context.Background())
	if err != nil {
		t.Fatalf("first List: %v", err)
	}
	second, err := rt.List(context.Background())
	if err != nil {
		t.Fatalf("second List: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("inner List calls = %d, want 1", got)
	}
	first[0].Labels["aima.dev/engine"] = "mutated"
	first[0].Config["port"] = 1
	if got := second[0].Labels["aima.dev/engine"]; got != "vllm" {
		t.Fatalf("cached labels were mutated, got %q", got)
	}
	if got := second[0].Config["port"]; got != 32100 {
		t.Fatalf("cached config was mutated, got %v", got)
	}
}

func TestCachedListRuntimeCoalescesConcurrentListCalls(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls int32
	rt := NewCachedListRuntime(&cachedListTestRuntime{
		name: "k3s",
		listFunc: func(ctx context.Context) ([]*DeploymentStatus, error) {
			atomic.AddInt32(&calls, 1)
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return []*DeploymentStatus{{Name: "aima-vllm", Phase: "running", Runtime: "k3s"}}, nil
		},
	}, time.Hour)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := rt.List(context.Background()); err != nil {
			t.Errorf("first List: %v", err)
		}
	}()

	<-started
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rt.List(context.Background()); err != nil {
				t.Errorf("coalesced List: %v", err)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("inner List calls = %d, want 1", got)
	}
}
