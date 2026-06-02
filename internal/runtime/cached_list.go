package runtime

import (
	"context"
	"errors"
	"sync"
	"time"
)

// NewCachedListRuntime wraps a runtime so repeated List calls share one
// in-flight query and reuse a short-lived result. Expensive runtimes such as
// K3S otherwise spawn a kubectl process for every UI/status poll.
func NewCachedListRuntime(inner Runtime, ttl time.Duration) Runtime {
	if inner == nil || ttl <= 0 {
		return inner
	}
	return &cachedListRuntime{
		inner: inner,
		ttl:   ttl,
	}
}

type cachedListRuntime struct {
	inner Runtime
	ttl   time.Duration

	mu        sync.Mutex
	cached    []*DeploymentStatus
	cachedAt  time.Time
	cachedErr error
	inFlight  chan struct{}
}

func (r *cachedListRuntime) Deploy(ctx context.Context, req *DeployRequest) error {
	err := r.inner.Deploy(ctx, req)
	r.invalidateList()
	return err
}

func (r *cachedListRuntime) Delete(ctx context.Context, name string) error {
	err := r.inner.Delete(ctx, name)
	r.invalidateList()
	return err
}

func (r *cachedListRuntime) Status(ctx context.Context, name string) (*DeploymentStatus, error) {
	return r.inner.Status(ctx, name)
}

func (r *cachedListRuntime) List(ctx context.Context) ([]*DeploymentStatus, error) {
	for {
		r.mu.Lock()
		if r.cacheFreshLocked(time.Now()) {
			statuses, err := cloneDeploymentStatuses(r.cached), r.cachedErr
			r.mu.Unlock()
			return statuses, err
		}
		if r.inFlight != nil {
			ch := r.inFlight
			r.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		ch := make(chan struct{})
		r.inFlight = ch
		r.mu.Unlock()

		statuses, err := r.inner.List(ctx)

		r.mu.Lock()
		if err == nil || cacheableListError(err) {
			r.cached = cloneDeploymentStatuses(statuses)
			r.cachedErr = err
			r.cachedAt = time.Now()
		} else {
			r.cached = nil
			r.cachedErr = nil
			r.cachedAt = time.Time{}
		}
		r.inFlight = nil
		close(ch)
		out, outErr := cloneDeploymentStatuses(r.cached), r.cachedErr
		r.mu.Unlock()
		if err != nil && !cacheableListError(err) {
			return nil, err
		}
		return out, outErr
	}
}

func (r *cachedListRuntime) Logs(ctx context.Context, name string, tailLines int) (string, error) {
	return r.inner.Logs(ctx, name, tailLines)
}

func (r *cachedListRuntime) Name() string {
	return r.inner.Name()
}

func (r *cachedListRuntime) invalidateList() {
	r.mu.Lock()
	r.cached = nil
	r.cachedErr = nil
	r.cachedAt = time.Time{}
	r.mu.Unlock()
}

func (r *cachedListRuntime) cacheFreshLocked(now time.Time) bool {
	return !r.cachedAt.IsZero() && now.Sub(r.cachedAt) < r.ttl
}

func cacheableListError(err error) bool {
	return err != nil &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded)
}

func cloneDeploymentStatuses(src []*DeploymentStatus) []*DeploymentStatus {
	if src == nil {
		return nil
	}
	out := make([]*DeploymentStatus, len(src))
	for i, status := range src {
		if status == nil {
			continue
		}
		cp := *status
		cp.Labels = cloneStringMap(status.Labels)
		cp.Config = cloneAnyMap(status.Config)
		out[i] = &cp
	}
	return out
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
