package driventest

import (
	"context"
	"sync"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

var _ driven.CIAdapter = (*StubCIAdapter)(nil)

type TriggerRunCall struct {
	JobName string
	Params  map[string]string
}

type PollRunCall struct {
	JobName string
	RunID   string
}

type ListJobsCall struct {
	JobName string
	RunID   string
}

type GetJobLogCall struct {
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
	QueueID           string
	Run               *domain.CIRun
	Jobs              []domain.CIJob
	Log               string
	Artifacts         []domain.CIArtifact
	Artifact          []byte
	BuildParams       map[string]string
	Builds            []domain.CIRun
	EstimatedDuration int64
	Err               error

	TriggerRunErr    error
	PollRunErr       error
	PollQueueErr     error
	ListJobsErr      error
	GetJobLogErr     error
	GetBuildParamsErr error
	ListBuildsErr    error
	ListArtifactsErr error
	GetArtifactErr   error

	mu                 sync.Mutex
	TriggerRunCalls    []TriggerRunCall
	PollRunCalls       []PollRunCall
	ListJobsCalls      []ListJobsCall
	GetJobLogCalls     []GetJobLogCall
	ListArtifactsCalls []ListArtifactsCall
	GetArtifactCalls   []GetArtifactCall
}

func NewStubCIAdapter(name string) *StubCIAdapter {
	return &StubCIAdapter{NameVal: name}
}

func (s *StubCIAdapter) Name() string { return s.NameVal }
func (s *StubCIAdapter) Type() string  { return "stub" }

func (s *StubCIAdapter) TriggerRun(_ context.Context, jobName string, params map[string]string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TriggerRunCalls = append(s.TriggerRunCalls, TriggerRunCall{JobName: jobName, Params: params})
	if s.TriggerRunErr != nil {
		return "", s.TriggerRunErr
	}
	if s.Err != nil {
		return "", s.Err
	}
	return s.RunID, nil
}

func (s *StubCIAdapter) PollRun(_ context.Context, jobName, runID string) (*domain.CIRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PollRunCalls = append(s.PollRunCalls, PollRunCall{JobName: jobName, RunID: runID})
	if s.PollRunErr != nil {
		return nil, s.PollRunErr
	}
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Run, nil
}

func (s *StubCIAdapter) PollQueue(_ context.Context, _ string) (string, error) {
	if s.PollQueueErr != nil {
		return "", s.PollQueueErr
	}
	return s.QueueID, nil
}

func (s *StubCIAdapter) ListJobs(_ context.Context, jobName, runID string) ([]domain.CIJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ListJobsCalls = append(s.ListJobsCalls, ListJobsCall{JobName: jobName, RunID: runID})
	if s.ListJobsErr != nil {
		return nil, s.ListJobsErr
	}
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Jobs, nil
}

func (s *StubCIAdapter) GetJobLog(_ context.Context, jobName, runID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GetJobLogCalls = append(s.GetJobLogCalls, GetJobLogCall{JobName: jobName, RunID: runID})
	if s.GetJobLogErr != nil {
		return "", s.GetJobLogErr
	}
	if s.Err != nil {
		return "", s.Err
	}
	return s.Log, nil
}

func (s *StubCIAdapter) GetBuildParams(_ context.Context, _, _ string) (map[string]string, error) {
	if s.GetBuildParamsErr != nil {
		return nil, s.GetBuildParamsErr
	}
	return s.BuildParams, nil
}

func (s *StubCIAdapter) ListBuilds(_ context.Context, _ string, _ int) ([]domain.CIRun, error) {
	if s.ListBuildsErr != nil {
		return nil, s.ListBuildsErr
	}
	return s.Builds, nil
}

func (s *StubCIAdapter) GetEstimatedDuration(_ context.Context, _ string) (int64, error) {
	return s.EstimatedDuration, nil
}

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

func (s *StubCIAdapter) CancelRun(_ context.Context, _, _ string) error { return s.Err }

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
