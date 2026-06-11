package cache

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

const (
	DefaultPollTTL     = 30 * time.Second
	DefaultListTTL     = 2 * time.Minute
	DefaultLogTTL      = 5 * time.Minute
	DefaultArtifactTTL = 10 * time.Minute
	DefaultCapacity    = 256
)

// compile-time assertions
var _ driven.CICore         = (*Adapter)(nil)
var _ driven.CITriggerable  = (*Adapter)(nil)
var _ driven.CIHistorical   = (*Adapter)(nil)
var _ driven.CIPipeliner    = (*Adapter)(nil)
var _ driven.CIArtifactStore = (*Adapter)(nil)
var _ driven.CIChainable    = (*Adapter)(nil)
var _ driven.Unwrapper      = (*Adapter)(nil)

type entry struct {
	key       string
	value     any
	expiresAt time.Time
}

type Adapter struct {
	inner driven.CICore

	mu       sync.Mutex
	items    map[string]*list.Element
	order    *list.List
	capacity int

	pollTTL     time.Duration
	listTTL     time.Duration
	logTTL      time.Duration
	artifactTTL time.Duration
	now         func() time.Time
}

type Option func(*Adapter)

func WithPollTTL(d time.Duration) Option     { return func(a *Adapter) { a.pollTTL = d } }
func WithListTTL(d time.Duration) Option     { return func(a *Adapter) { a.listTTL = d } }
func WithLogTTL(d time.Duration) Option      { return func(a *Adapter) { a.logTTL = d } }
func WithArtifactTTL(d time.Duration) Option { return func(a *Adapter) { a.artifactTTL = d } }
func WithCapacity(n int) Option              { return func(a *Adapter) { a.capacity = n } }

func New(inner driven.CICore, opts ...Option) *Adapter {
	a := &Adapter{
		inner:       inner,
		items:       make(map[string]*list.Element),
		order:       list.New(),
		capacity:    DefaultCapacity,
		pollTTL:     DefaultPollTTL,
		listTTL:     DefaultListTTL,
		logTTL:      DefaultLogTTL,
		artifactTTL: DefaultArtifactTTL,
		now:         time.Now,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// ── CICore ───────────────────────────────────────────────────────────────────

func (a *Adapter) Name() string                    { return a.inner.Name() }
func (a *Adapter) Type() string                    { return a.inner.Type() }
func (a *Adapter) Capabilities() driven.CapabilitySet { return a.inner.Capabilities() }

func (a *Adapter) GetRun(ctx context.Context, jobRef string, runID string) (*domain.CIRun, error) {
	key := fmt.Sprintf("poll:%s:%s", jobRef, runID)

	if v, ok := a.get(key); ok {
		run := v.(*domain.CIRun)
		if run.Status.IsTerminal() {
			return run, nil
		}
	}

	run, err := a.inner.GetRun(ctx, jobRef, runID)
	if err != nil {
		return nil, err
	}

	ttl := a.pollTTL
	if run.Status.IsTerminal() {
		ttl = a.logTTL
	}
	a.put(key, run, ttl)
	return run, nil
}

func (a *Adapter) SearchRuns(ctx context.Context, jobRef string, f domain.BuildFilter) ([]domain.CIRun, error) {
	// Not cached — results depend on dynamic filter criteria.
	return a.inner.SearchRuns(ctx, jobRef, f)
}

func (a *Adapter) GetLog(ctx context.Context, jobRef string, runID string, f domain.LogFilter) (string, error) {
	key := fmt.Sprintf("log:%s:%s", jobRef, runID)
	if v, ok := a.get(key); ok {
		return v.(string), nil
	}
	log, err := a.inner.GetLog(ctx, jobRef, runID, f)
	if err != nil {
		return "", err
	}
	a.put(key, log, a.logTTL)
	return log, nil
}

func (a *Adapter) CancelRun(ctx context.Context, jobRef string, runID string) error {
	a.invalidatePrefix(fmt.Sprintf("poll:%s:%s", jobRef, runID))
	return a.inner.CancelRun(ctx, jobRef, runID)
}

// ── Unwrapper — exposes inner for driven.As[T] capability detection ──────────

func (a *Adapter) Unwrap() driven.CICore { return a.inner }

// ── CITriggerable — delegates to inner; invalidates poll cache on trigger ────

func (a *Adapter) Trigger(ctx context.Context, jobRef string, params map[string]string) (*domain.TriggerReceipt, error) {
	if t, ok := a.inner.(driven.CITriggerable); ok {
		a.invalidatePrefix("poll:" + jobRef)
		return t.Trigger(ctx, jobRef, params)
	}
	return nil, fmt.Errorf("backend %q does not support triggering (capabilities: %v)",
		a.inner.Name(), a.inner.Capabilities())
}

func (a *Adapter) ResolveReceipt(ctx context.Context, r *domain.TriggerReceipt) (*domain.TriggerReceipt, error) {
	if t, ok := a.inner.(driven.CITriggerable); ok {
		return t.ResolveReceipt(ctx, r)
	}
	return r, fmt.Errorf("backend %q does not support trigger resolution", a.inner.Name())
}

func (a *Adapter) EstimateDuration(ctx context.Context, jobRef string) (int64, error) {
	key := fmt.Sprintf("estdur:%s", jobRef)
	if v, ok := a.get(key); ok {
		return v.(int64), nil
	}
	if t, ok := a.inner.(driven.CITriggerable); ok {
		d, err := t.EstimateDuration(ctx, jobRef)
		if err != nil {
			return 0, err
		}
		a.put(key, d, a.logTTL)
		return d, nil
	}
	return 0, nil
}

// ── CIHistorical ─────────────────────────────────────────────────────────────

func (a *Adapter) ListRuns(ctx context.Context, jobRef string, limit int) ([]domain.CIRun, error) {
	key := fmt.Sprintf("runs:%s:%d", jobRef, limit)
	if v, ok := a.get(key); ok {
		return v.([]domain.CIRun), nil
	}
	if h, ok := a.inner.(driven.CIHistorical); ok {
		runs, err := h.ListRuns(ctx, jobRef, limit)
		if err != nil {
			return nil, err
		}
		a.put(key, runs, a.listTTL)
		return runs, nil
	}
	return nil, fmt.Errorf("backend %q does not support run history", a.inner.Name())
}

func (a *Adapter) GetRunParams(ctx context.Context, jobRef, runID string) (map[string]string, error) {
	key := fmt.Sprintf("params:%s:%s", jobRef, runID)
	if v, ok := a.get(key); ok {
		return v.(map[string]string), nil
	}
	if h, ok := a.inner.(driven.CIHistorical); ok {
		params, err := h.GetRunParams(ctx, jobRef, runID)
		if err != nil {
			return nil, err
		}
		a.put(key, params, a.logTTL)
		return params, nil
	}
	return nil, fmt.Errorf("backend %q does not support run params", a.inner.Name())
}

// ── CIPipeliner ──────────────────────────────────────────────────────────────

func (a *Adapter) ListStages(ctx context.Context, jobRef, runID string) ([]domain.CIJob, error) {
	key := fmt.Sprintf("stages:%s:%s", jobRef, runID)
	if v, ok := a.get(key); ok {
		return v.([]domain.CIJob), nil
	}
	if p, ok := a.inner.(driven.CIPipeliner); ok {
		stages, err := p.ListStages(ctx, jobRef, runID)
		if err != nil {
			return nil, err
		}
		a.put(key, stages, a.listTTL)
		return stages, nil
	}
	return nil, fmt.Errorf("backend %q does not support pipeline stages", a.inner.Name())
}

// ── CIArtifactStore ──────────────────────────────────────────────────────────

func (a *Adapter) ListStageNodes(ctx context.Context, jobRef, runID string) ([]domain.CIStageNode, error) {
	// Not cached — steps expand per-stage via extra HTTP calls; caching would need per-stage keys.
	if p, ok := a.inner.(driven.CIPipeliner); ok {
		return p.ListStageNodes(ctx, jobRef, runID)
	}
	return nil, fmt.Errorf("backend %q does not support stage nodes", a.inner.Name())
}

func (a *Adapter) ListWfArtifacts(ctx context.Context, jobRef, runID string) ([]domain.CIArtifact, error) {
	key := fmt.Sprintf("wf_artifacts:%s:%s", jobRef, runID)
	if v, ok := a.get(key); ok {
		return v.([]domain.CIArtifact), nil
	}
	if s, ok := a.inner.(driven.CIArtifactStore); ok {
		artifacts, err := s.ListWfArtifacts(ctx, jobRef, runID)
		if err != nil {
			return nil, err
		}
		a.put(key, artifacts, a.artifactTTL)
		return artifacts, nil
	}
	return nil, fmt.Errorf("backend %q does not support wf artifacts", a.inner.Name())
}

func (a *Adapter) ListArtifacts(ctx context.Context, jobRef, runID string) ([]domain.CIArtifact, error) {
	key := fmt.Sprintf("artifacts:%s:%s", jobRef, runID)
	if v, ok := a.get(key); ok {
		return v.([]domain.CIArtifact), nil
	}
	if s, ok := a.inner.(driven.CIArtifactStore); ok {
		artifacts, err := s.ListArtifacts(ctx, jobRef, runID)
		if err != nil {
			return nil, err
		}
		a.put(key, artifacts, a.artifactTTL)
		return artifacts, nil
	}
	return nil, fmt.Errorf("backend %q does not support artifacts", a.inner.Name())
}

func (a *Adapter) GetArtifact(ctx context.Context, jobRef, runID, path string) ([]byte, error) {
	key := fmt.Sprintf("artifact:%s:%s:%s", jobRef, runID, path)
	if v, ok := a.get(key); ok {
		return v.([]byte), nil
	}
	if s, ok := a.inner.(driven.CIArtifactStore); ok {
		data, err := s.GetArtifact(ctx, jobRef, runID, path)
		if err != nil {
			return nil, err
		}
		a.put(key, data, a.artifactTTL)
		return data, nil
	}
	return nil, fmt.Errorf("backend %q does not support artifact download", a.inner.Name())
}

// ── CIChainable — not cached (time-sensitive, filter-dependent) ───────────────

func (a *Adapter) GetDownstreamRuns(ctx context.Context, downstreamJob, upstreamJob, upstreamRunID string) ([]domain.CIRun, error) {
	if c, ok := a.inner.(driven.CIChainable); ok {
		return c.GetDownstreamRuns(ctx, downstreamJob, upstreamJob, upstreamRunID)
	}
	return nil, fmt.Errorf("backend %q does not support chain traversal", a.inner.Name())
}

// ── LRU cache internals ───────────────────────────────────────────────────────

func (a *Adapter) get(key string) (any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	el, ok := a.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*entry)
	if a.now().After(e.expiresAt) {
		a.order.Remove(el)
		delete(a.items, key)
		return nil, false
	}
	a.order.MoveToFront(el)
	return e.value, true
}

func (a *Adapter) put(key string, value any, ttl time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if el, ok := a.items[key]; ok {
		a.order.MoveToFront(el)
		e := el.Value.(*entry)
		e.value = value
		e.expiresAt = a.now().Add(ttl)
		return
	}
	for a.order.Len() >= a.capacity {
		back := a.order.Back()
		if back == nil {
			break
		}
		a.order.Remove(back)
		delete(a.items, back.Value.(*entry).key)
	}
	e := &entry{key: key, value: value, expiresAt: a.now().Add(ttl)}
	el := a.order.PushFront(e)
	a.items[key] = el
}

func (a *Adapter) invalidatePrefix(prefix string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, el := range a.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			a.order.Remove(el)
			delete(a.items, k)
		}
	}
}
