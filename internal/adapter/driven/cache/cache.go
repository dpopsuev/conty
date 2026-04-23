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

var _ driven.CIAdapter = (*Adapter)(nil)

type entry struct {
	key       string
	value     any
	expiresAt time.Time
}

type Adapter struct {
	inner driven.CIAdapter

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

func New(inner driven.CIAdapter, opts ...Option) *Adapter {
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

func (a *Adapter) Name() string { return a.inner.Name() }

func (a *Adapter) TriggerRun(ctx context.Context, jobName string, params map[string]string) (string, error) {
	a.invalidatePrefix(fmt.Sprintf("poll:%s:", jobName))
	return a.inner.TriggerRun(ctx, jobName, params)
}

func (a *Adapter) PollRun(ctx context.Context, jobName string, runID string) (*domain.CIRun, error) {
	key := fmt.Sprintf("poll:%s:%s", jobName, runID)

	if v, ok := a.get(key); ok {
		run := v.(*domain.CIRun)
		if run.Status.IsTerminal() {
			return run, nil
		}
	}

	run, err := a.inner.PollRun(ctx, jobName, runID)
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

func (a *Adapter) ListJobs(ctx context.Context, jobName string, runID string) ([]domain.CIJob, error) {
	key := fmt.Sprintf("jobs:%s:%s", jobName, runID)
	if v, ok := a.get(key); ok {
		return v.([]domain.CIJob), nil
	}
	jobs, err := a.inner.ListJobs(ctx, jobName, runID)
	if err != nil {
		return nil, err
	}
	a.put(key, jobs, a.listTTL)
	return jobs, nil
}

func (a *Adapter) GetJobLog(ctx context.Context, jobName string, runID string) (string, error) {
	key := fmt.Sprintf("log:%s:%s", jobName, runID)
	if v, ok := a.get(key); ok {
		return v.(string), nil
	}
	log, err := a.inner.GetJobLog(ctx, jobName, runID)
	if err != nil {
		return "", err
	}
	a.put(key, log, a.logTTL)
	return log, nil
}

func (a *Adapter) ListArtifacts(ctx context.Context, jobName string, runID string) ([]domain.CIArtifact, error) {
	key := fmt.Sprintf("artifacts:%s:%s", jobName, runID)
	if v, ok := a.get(key); ok {
		return v.([]domain.CIArtifact), nil
	}
	artifacts, err := a.inner.ListArtifacts(ctx, jobName, runID)
	if err != nil {
		return nil, err
	}
	a.put(key, artifacts, a.artifactTTL)
	return artifacts, nil
}

func (a *Adapter) GetArtifact(ctx context.Context, jobName string, runID string, path string) ([]byte, error) {
	key := fmt.Sprintf("artifact:%s:%s:%s", jobName, runID, path)
	if v, ok := a.get(key); ok {
		return v.([]byte), nil
	}
	data, err := a.inner.GetArtifact(ctx, jobName, runID, path)
	if err != nil {
		return nil, err
	}
	a.put(key, data, a.artifactTTL)
	return data, nil
}

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
