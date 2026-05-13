package drivertest

import (
	"context"
	"sync"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driver"
)

var _ = domain.TriggerResult{}

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

func (s *StubPipelineService) ListBackends() []string {
	return nil
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
	Params  map[string]string
}

type CITriggerCall struct {
	Backend string
	JobRef  string
	Params  map[string]string
}

type CIParamsCall struct {
	Backend string
	JobRef  string
	RunID   string
}

type CIHistoryCall struct {
	Backend string
	JobRef  string
	Limit   int
}

type CILogCall struct {
	Backend string
	JobRef  string
	RunID   string
}

type CIPollCall struct {
	Backend string
	QueueID string
}

type CIWatchCall struct {
	Backend string
	JobRef  string
	RunID   string
}

type CIArtifactsCall struct {
	Backend string
	JobRef  string
	RunID   string
}

type CIArtifactGetCall struct {
	Backend string
	JobRef  string
	RunID   string
	Path    string
}

type CICancelCall struct {
	Backend string
	RunID   string
}

type StubCIMonitorService struct {
	Check         *domain.CICheck
	Verdict       *domain.CIVerdict
	RunID         string
	TriggerResult *domain.TriggerResult
	Params        map[string]string
	Builds        []domain.CIRun
	Log           string
	WatchStatus   *domain.WatchStatus
	Artifacts     []domain.CIArtifact
	Artifact      []byte
	BackendInfos  []domain.BackendInfo
	Owned         []domain.OwnedRun
	Err           error

	mu                sync.Mutex
	CheckCalls        []CheckLatestCall
	VerdictCalls      []GetVerdictCall
	RedeployCalls     []RedeployCall
	CITriggerCalls    []CITriggerCall
	CIParamsCalls     []CIParamsCall
	CIHistoryCalls    []CIHistoryCall
	CILogCalls        []CILogCall
	CIPollCalls       []CIPollCall
	CIWatchCalls      []CIWatchCall
	CIArtifactsCalls  []CIArtifactsCall
	CIArtifactGetCalls []CIArtifactGetCall
	CICancelCalls     []CICancelCall
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

func (s *StubCIMonitorService) TriggerRedeployWithParams(_ context.Context, backend, jobRef string, params map[string]string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RedeployCalls = append(s.RedeployCalls, RedeployCall{Backend: backend, JobRef: jobRef, Params: params})
	return s.RunID, s.Err
}

func (s *StubCIMonitorService) CITrigger(_ context.Context, backend, jobRef string, params map[string]string) (*domain.TriggerResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CITriggerCalls = append(s.CITriggerCalls, CITriggerCall{Backend: backend, JobRef: jobRef, Params: params})
	return s.TriggerResult, s.Err
}

func (s *StubCIMonitorService) CIParams(_ context.Context, backend, jobRef, runID string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CIParamsCalls = append(s.CIParamsCalls, CIParamsCall{Backend: backend, JobRef: jobRef, RunID: runID})
	return s.Params, s.Err
}

func (s *StubCIMonitorService) CIHistory(_ context.Context, backend, jobRef string, limit int) ([]domain.CIRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CIHistoryCalls = append(s.CIHistoryCalls, CIHistoryCall{Backend: backend, JobRef: jobRef, Limit: limit})
	return s.Builds, s.Err
}

func (s *StubCIMonitorService) CILog(_ context.Context, backend, jobRef, runID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CILogCalls = append(s.CILogCalls, CILogCall{Backend: backend, JobRef: jobRef, RunID: runID})
	return s.Log, s.Err
}

func (s *StubCIMonitorService) CIPoll(_ context.Context, backend, queueID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CIPollCalls = append(s.CIPollCalls, CIPollCall{Backend: backend, QueueID: queueID})
	return s.RunID, s.Err
}

func (s *StubCIMonitorService) CIWatch(_ context.Context, backend, jobRef, runID string) (*domain.WatchStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CIWatchCalls = append(s.CIWatchCalls, CIWatchCall{Backend: backend, JobRef: jobRef, RunID: runID})
	return s.WatchStatus, s.Err
}

func (s *StubCIMonitorService) OwnsRun(_, _ string) bool {
	return false
}

func (s *StubCIMonitorService) ListOwnedRuns() []domain.OwnedRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Owned
}

func (s *StubCIMonitorService) CIArtifacts(_ context.Context, backend, jobRef, runID string) ([]domain.CIArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CIArtifactsCalls = append(s.CIArtifactsCalls, CIArtifactsCall{Backend: backend, JobRef: jobRef, RunID: runID})
	return s.Artifacts, s.Err
}

func (s *StubCIMonitorService) CIArtifactGet(_ context.Context, backend, jobRef, runID, path string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CIArtifactGetCalls = append(s.CIArtifactGetCalls, CIArtifactGetCall{Backend: backend, JobRef: jobRef, RunID: runID, Path: path})
	return s.Artifact, s.Err
}

func (s *StubCIMonitorService) BackendInfo() []domain.BackendInfo {
	return s.BackendInfos
}

func (s *StubCIMonitorService) CICancel(_ context.Context, backend, jobRef, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CICancelCalls = append(s.CICancelCalls, CICancelCall{Backend: backend, RunID: runID})
	_ = jobRef
	return s.Err
}
