package driven

import (
	"context"
	"strings"

	"github.com/dpopsuev/conty/internal/domain"
)

// CapabilitySet is a bitmask describing which optional interfaces a backend
// supports. Returned by CICore.Capabilities() and used by service guards and
// help text.
type CapabilitySet uint

const (
	CapTrigger   CapabilitySet = 1 << iota // CITriggerable
	CapHistory                              // CIHistorical
	CapStages                              // CIPipeliner
	CapArtifacts                           // CIArtifactStore
	CapChain                              // CIChainable
)

// Has reports whether all bits in cap are set.
func (c CapabilitySet) Has(cap CapabilitySet) bool { return c&cap == cap }

// String returns a human-readable representation for use in help text and
// error messages. Example: "trigger history stages artifacts chain".
func (c CapabilitySet) String() string {
	if c == 0 {
		return "none"
	}
	var parts []string
	if c.Has(CapTrigger) {
		parts = append(parts, "trigger")
	}
	if c.Has(CapHistory) {
		parts = append(parts, "history")
	}
	if c.Has(CapStages) {
		parts = append(parts, "stages")
	}
	if c.Has(CapArtifacts) {
		parts = append(parts, "artifacts")
	}
	if c.Has(CapChain) {
		parts = append(parts, "chain")
	}
	return strings.Join(parts, " ")
}

// ────────────────────────────────────────────────────────────────────────────
// Core interface — mandatory minimum, every backend must implement.
// ────────────────────────────────────────────────────────────────────────────

// CICore is the mandatory minimum interface every CI backend must satisfy.
// It covers read-only observation of runs plus cancel, and is intentionally
// kept narrow so that new backends (e.g. a read-only Prow skeleton) can be
// wired in without stub methods.
type CICore interface {
	Name() string
	Type() string
	// Capabilities returns the set of optional interfaces this backend
	// supports. Used for help text and service-layer guard clauses.
	Capabilities() CapabilitySet

	// GetRun fetches the current state of a single run. runID "latest" returns
	// the most recent run. Replaces PollRun.
	GetRun(ctx context.Context, jobRef, runID string) (*domain.CIRun, error)

	// SearchRuns returns past runs matching the filter. Replaces SearchBuilds.
	SearchRuns(ctx context.Context, jobRef string, f domain.BuildFilter) ([]domain.CIRun, error)

	// GetLog returns the console log for a run. Backends return the raw log;
	// the LogFilter is a hint for backends that support server-side filtering.
	// Replaces GetJobLog.
	GetLog(ctx context.Context, jobRef, runID string, f domain.LogFilter) (string, error)

	// CancelRun aborts a run. Implementations must be idempotent.
	CancelRun(ctx context.Context, jobRef, runID string) error
}

// ────────────────────────────────────────────────────────────────────────────
// Optional capability interfaces — discovered via As[T] or Capabilities().
// ────────────────────────────────────────────────────────────────────────────

// CITriggerable is implemented by backends that support triggering new runs.
// It merges the old TriggerRun + PollQueue + GetEstimatedDuration into a
// single cohesion group. Backends that assign a run ID synchronously return
// NeedsResolve=false from Trigger; async backends return NeedsResolve=true
// and must support ResolveReceipt polling.
type CITriggerable interface {
	Trigger(ctx context.Context, jobRef string, params map[string]string) (*domain.TriggerReceipt, error)
	ResolveReceipt(ctx context.Context, r *domain.TriggerReceipt) (*domain.TriggerReceipt, error)
	EstimateDuration(ctx context.Context, jobRef string) (int64, error)
}

// CIHistorical provides access to bulk historical run data.
// Replaces ListBuilds + GetBuildParams.
type CIHistorical interface {
	ListRuns(ctx context.Context, jobRef string, limit int) ([]domain.CIRun, error)
	GetRunParams(ctx context.Context, jobRef, runID string) (map[string]string, error)
}

// CIPipeliner exposes the sub-run structure of a build (pipeline stages,
// GitHub jobs-in-run, GitLab jobs-in-pipeline). Backends with flat job
// models (Prow) do not implement this interface.
// Replaces ListJobs.
type CIPipeliner interface {
	ListStages(ctx context.Context, jobRef, runID string) ([]domain.CIJob, error)
}

// CIArtifactStore provides artifact listing and download.
type CIArtifactStore interface {
	ListArtifacts(ctx context.Context, jobRef, runID string) ([]domain.CIArtifact, error)
	GetArtifact(ctx context.Context, jobRef, runID, path string) ([]byte, error)
}

// CIChainable provides upstream/downstream build chain traversal.
// Jenkins uses cause-chain inspection; GitLab uses the bridges API.
// GitHub has no native reverse index and does not implement this interface.
type CIChainable interface {
	GetDownstreamRuns(ctx context.Context, downstreamJob, upstreamJob, upstreamRunID string) ([]domain.CIRun, error)
}

// ────────────────────────────────────────────────────────────────────────────
// Unwrap + As[T] — transparent proxy support for the cache decorator.
// ────────────────────────────────────────────────────────────────────────────

// Unwrapper is implemented by decorator adapters (e.g. cache.Adapter) to
// expose the inner CICore. As[T] uses this to peel through wrapper layers
// and reach the real backend for capability detection.
type Unwrapper interface {
	Unwrap() CICore
}

// As[T] walks the Unwrapper chain to find the first adapter that implements T.
// Prefer peeling through wrappers to reach the real backend — this ensures
// capability checks reflect the underlying adapter, not the wrapper's
// delegation stubs.
//
//	_, ok := driven.As[driven.CIChainable](cache.New(githubAdapter)) // false
//	_, ok := driven.As[driven.CITriggerable](cache.New(jenkinsAdapter)) // true
func As[T any](a CICore) (T, bool) {
	var zero T
	if a == nil {
		return zero, false
	}
	// Prefer the unwrapped (real) adapter over wrapper delegation stubs.
	if w, ok := a.(Unwrapper); ok {
		return As[T](w.Unwrap())
	}
	if t, ok := any(a).(T); ok {
		return t, true
	}
	return zero, false
}
