package driventest

import (
	"context"
	"sync"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

// compile-time assertions — StubCIAdapter satisfies CICore plus all optional interfaces.
var _ driven.CICore         = (*StubCIAdapter)(nil)
var _ driven.CITriggerable  = (*StubCIAdapter)(nil)
var _ driven.CIHistorical   = (*StubCIAdapter)(nil)
var _ driven.CIPipeliner    = (*StubCIAdapter)(nil)
var _ driven.CIArtifactStore = (*StubCIAdapter)(nil)
var _ driven.CIChainable    = (*StubCIAdapter)(nil)

type TriggerRunCall struct {
	JobName string
	Params  map[string]string
}

type GetRunCall struct {
	JobName string
	RunID   string
}

type ListStagesCall struct {
	JobName string
	RunID   string
}

type GetLogCall struct {
	JobName string
	RunID   string
}

type ListArtifactsCall struct {
	JobName string
	RunID   string
}

type GetArtifactCall struct {
	JobName string
	RunID   string
	Path    string
}

type StubCIAdapter struct {
	NameVal           string
	RunID             string
	QueueID           string // kept for backward compat; maps to TriggerReceipt.OpaqueRef
	Run               *domain.CIRun
	Jobs              []domain.CIJob
	Log               string
	Artifacts         []domain.CIArtifact
	Artifact          []byte
	BuildParams       map[string]string
	Builds            []domain.CIRun
	DownstreamRuns    []domain.CIRun
	EstimatedDuration int64
	Err               error

	TriggerRunErr        error
	GetRunErr            error
	ListStagesErr        error
	StageNodes           []domain.CIStageNode
	ListStageNodesErr    error
	GetLogErr            error
	GetBuildParamsErr    error
	ListBuildsErr        error
	ListArtifactsErr     error
	GetArtifactErr       error
	WfArtifacts          []domain.CIArtifact
	ListWfArtifactsErr   error
	GetDownstreamRunsErr error

	mu                 sync.Mutex
	TriggerRunCalls    []TriggerRunCall
	GetRunCalls        []GetRunCall
	ListStagesCalls    []ListStagesCall
	GetLogCalls        []GetLogCall
	ListArtifactsCalls []ListArtifactsCall
	GetArtifactCalls   []GetArtifactCall
}

func NewStubCIAdapter(name string) *StubCIAdapter {
	return &StubCIAdapter{NameVal: name}
}

func (s *StubCIAdapter) Name() string { return s.NameVal }
func (s *StubCIAdapter) Type() string  { return "stub" }
func (s *StubCIAdapter) Capabilities() driven.CapabilitySet {
	return driven.CapTrigger | driven.CapHistory | driven.CapStages | driven.CapArtifacts | driven.CapChain
}

// ── CICore ───────────────────────────────────────────────────────────────────

func (s *StubCIAdapter) GetRun(_ context.Context, jobName, runID string) (*domain.CIRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GetRunCalls = append(s.GetRunCalls, GetRunCall{JobName: jobName, RunID: runID})
	if s.GetRunErr != nil {
		return nil, s.GetRunErr
	}
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Run, nil
}

func (s *StubCIAdapter) SearchRuns(_ context.Context, _ string, f domain.BuildFilter) ([]domain.CIRun, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	var out []domain.CIRun
	for _, b := range s.Builds {
		if f.Result != "" && string(b.Result) != f.Result {
			continue
		}
		if !f.Since.IsZero() && b.StartedAt.Before(f.Since) {
			continue
		}
		out = append(out, b)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

func (s *StubCIAdapter) GetLog(_ context.Context, jobName, runID string, _ domain.LogFilter) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GetLogCalls = append(s.GetLogCalls, GetLogCall{JobName: jobName, RunID: runID})
	if s.GetLogErr != nil {
		return "", s.GetLogErr
	}
	if s.Err != nil {
		return "", s.Err
	}
	return s.Log, nil
}

func (s *StubCIAdapter) CancelRun(_ context.Context, _, _ string) error { return s.Err }

// ── CITriggerable ────────────────────────────────────────────────────────────

func (s *StubCIAdapter) Trigger(_ context.Context, jobName string, params map[string]string) (*domain.TriggerReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TriggerRunCalls = append(s.TriggerRunCalls, TriggerRunCall{JobName: jobName, Params: params})
	if s.TriggerRunErr != nil {
		return nil, s.TriggerRunErr
	}
	if s.Err != nil {
		return nil, s.Err
	}
	runID := s.RunID
	needsResolve := runID == ""
	opaqueRef := s.QueueID
	return &domain.TriggerReceipt{
		RunID:        runID,
		OpaqueRef:    opaqueRef,
		NeedsResolve: needsResolve,
	}, nil
}

func (s *StubCIAdapter) ResolveReceipt(_ context.Context, r *domain.TriggerReceipt) (*domain.TriggerReceipt, error) {
	if s.Err != nil {
		return r, s.Err
	}
	if s.QueueID != "" && s.RunID != "" {
		resolved := *r
		resolved.RunID = s.RunID
		resolved.NeedsResolve = false
		return &resolved, nil
	}
	return r, nil
}

func (s *StubCIAdapter) EstimateDuration(_ context.Context, _ string) (int64, error) {
	return s.EstimatedDuration, nil
}

// ── CIHistorical ─────────────────────────────────────────────────────────────

func (s *StubCIAdapter) ListRuns(_ context.Context, _ string, _ int) ([]domain.CIRun, error) {
	if s.ListBuildsErr != nil {
		return nil, s.ListBuildsErr
	}
	return s.Builds, nil
}

func (s *StubCIAdapter) GetRunParams(_ context.Context, _, _ string) (map[string]string, error) {
	if s.GetBuildParamsErr != nil {
		return nil, s.GetBuildParamsErr
	}
	return s.BuildParams, nil
}

// ── CIPipeliner ──────────────────────────────────────────────────────────────

func (s *StubCIAdapter) ListStageNodes(_ context.Context, _, _ string) ([]domain.CIStageNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ListStageNodesErr != nil {
		return nil, s.ListStageNodesErr
	}
	return s.StageNodes, nil
}

func (s *StubCIAdapter) ListStageNodesWithLogs(_ context.Context, _, _ string) ([]domain.CIStageNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ListStageNodesErr != nil {
		return nil, s.ListStageNodesErr
	}
	return s.StageNodes, nil
}

func (s *StubCIAdapter) ListWfArtifacts(_ context.Context, _, _ string) ([]domain.CIArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ListWfArtifactsErr != nil {
		return nil, s.ListWfArtifactsErr
	}
	return s.WfArtifacts, nil
}

func (s *StubCIAdapter) ListStages(_ context.Context, jobName, runID string) ([]domain.CIJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ListStagesCalls = append(s.ListStagesCalls, ListStagesCall{JobName: jobName, RunID: runID})
	if s.ListStagesErr != nil {
		return nil, s.ListStagesErr
	}
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Jobs, nil
}

// ── CIArtifactStore ──────────────────────────────────────────────────────────

func (s *StubCIAdapter) ListArtifacts(_ context.Context, jobName, runID string) ([]domain.CIArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ListArtifactsCalls = append(s.ListArtifactsCalls, ListArtifactsCall{JobName: jobName, RunID: runID})
	if s.ListArtifactsErr != nil {
		return nil, s.ListArtifactsErr
	}
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Artifacts, nil
}

func (s *StubCIAdapter) GetArtifact(_ context.Context, jobName, runID, path string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GetArtifactCalls = append(s.GetArtifactCalls, GetArtifactCall{JobName: jobName, RunID: runID, Path: path})
	if s.GetArtifactErr != nil {
		return nil, s.GetArtifactErr
	}
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Artifact, nil
}

// ── CIChainable ──────────────────────────────────────────────────────────────

func (s *StubCIAdapter) GetDownstreamRuns(_ context.Context, _, _, _ string) ([]domain.CIRun, error) {
	if s.GetDownstreamRunsErr != nil {
		return nil, s.GetDownstreamRunsErr
	}
	return s.DownstreamRuns, nil
}
