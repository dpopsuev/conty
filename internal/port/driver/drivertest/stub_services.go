package drivertest

import (
	"context"
	"sync"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driver"
)

var _ driver.PipelineService = (*StubPipelineService)(nil)
var _ driver.CIMonitorService = (*StubCIMonitorService)(nil)

type PipelineTriggerCall struct {
	Name string
}

type PipelineStatusCall struct {
	Name string
}

type StepLogCall struct {
	Name string
	Step int
}

type StubPipelineService struct {
	PipelineRun *domain.PipelineRun
	StepLog     string
	Pipelines   []string
	Err         error

	mu                sync.Mutex
	TriggerCalls      []PipelineTriggerCall
	StatusCalls       []PipelineStatusCall
	StepLogCalls      []StepLogCall
}

func (s *StubPipelineService) TriggerPipeline(_ context.Context, name string) (*domain.PipelineRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TriggerCalls = append(s.TriggerCalls, PipelineTriggerCall{Name: name})
	return s.PipelineRun, s.Err
}

func (s *StubPipelineService) GetPipelineStatus(_ context.Context, name string) (*domain.PipelineRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.StatusCalls = append(s.StatusCalls, PipelineStatusCall{Name: name})
	return s.PipelineRun, s.Err
}

func (s *StubPipelineService) GetStepLog(_ context.Context, name string, step int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.StepLogCalls = append(s.StepLogCalls, StepLogCall{Name: name, Step: step})
	return s.StepLog, s.Err
}

func (s *StubPipelineService) ListPipelines() []string {
	return s.Pipelines
}

type CheckLatestCall struct {
	Backend string
	JobRef  string
}

type GetVerdictCall struct {
	Backend string
	JobRef  string
}

type RedeployCall struct {
	Backend string
	JobRef  string
}

type StubCIMonitorService struct {
	Check   *domain.CICheck
	Verdict *domain.CIVerdict
	RunID   string
	Err     error

	mu             sync.Mutex
	CheckCalls     []CheckLatestCall
	VerdictCalls   []GetVerdictCall
	RedeployCalls  []RedeployCall
}

func (s *StubCIMonitorService) CheckLatest(_ context.Context, backend, jobRef string) (*domain.CICheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CheckCalls = append(s.CheckCalls, CheckLatestCall{Backend: backend, JobRef: jobRef})
	return s.Check, s.Err
}

func (s *StubCIMonitorService) GetVerdict(_ context.Context, backend, jobRef string) (*domain.CIVerdict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VerdictCalls = append(s.VerdictCalls, GetVerdictCall{Backend: backend, JobRef: jobRef})
	return s.Verdict, s.Err
}

func (s *StubCIMonitorService) TriggerRedeploy(_ context.Context, backend, jobRef string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RedeployCalls = append(s.RedeployCalls, RedeployCall{Backend: backend, JobRef: jobRef})
	return s.RunID, s.Err
}
