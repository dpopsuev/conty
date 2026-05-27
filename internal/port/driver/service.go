package driver

import (
	"context"

	"github.com/dpopsuev/conty/internal/domain"
)

type PipelineService interface {
	TriggerPipeline(ctx context.Context, name string) (*domain.PipelineRun, error)
	GetPipelineStatus(ctx context.Context, name string) (*domain.PipelineRun, error)
	GetStepLog(ctx context.Context, name string, step int, f domain.LogFilter) (domain.LogResult, error)
	ListPipelines() []string
	ListBackends() []string
}

type CIMonitorService interface {
	CheckLatest(ctx context.Context, backend, jobRef string) (*domain.CICheck, error)
	GetVerdict(ctx context.Context, backend, jobRef, runID string, f domain.LogFilter) (*domain.CIVerdict, error)
	TriggerRedeploy(ctx context.Context, backend, jobRef string) (string, error)
	TriggerRedeployWithParams(ctx context.Context, backend, jobRef string, params map[string]string) (string, error)
	CITrigger(ctx context.Context, backend, jobRef string, params map[string]string) (*domain.TriggerResult, error)
	CIParams(ctx context.Context, backend, jobRef, runID string) (map[string]string, error)
	CIHistory(ctx context.Context, backend, jobRef string, limit int) ([]domain.CIRun, error)
	CISearch(ctx context.Context, backend, jobRef string, f domain.BuildFilter) ([]domain.CIRun, error)
	CILog(ctx context.Context, backend, jobRef, runID string, f domain.LogFilter) (domain.LogResult, error)
	CIPoll(ctx context.Context, backend, queueID string) (string, error)
	CIWatch(ctx context.Context, backend, jobRef, runID string) (*domain.WatchStatus, error)
	CIArtifacts(ctx context.Context, backend, jobRef, runID string) ([]domain.CIArtifact, error)
	CIArtifactGet(ctx context.Context, backend, jobRef, runID, path string) ([]byte, error)
	CIArtifactText(ctx context.Context, backend, jobRef, runID, path string, f domain.LogFilter) (domain.LogResult, error)
	CIParamsTruncated(ctx context.Context, backend, jobRef, runID string) (map[string]string, []string, error)
	CICancel(ctx context.Context, backend, jobRef, runID string) error
	CIGetRun(ctx context.Context, backend, jobRef, runID string) (*domain.CIRun, error)
	CIDownstream(ctx context.Context, backend, downstreamJob, upstreamJob, upstreamRunID string) ([]domain.CIRun, error)
	OwnsRun(backend, buildNumber string) bool
	ListOwnedRuns() []domain.OwnedRun
	BackendInfo() []domain.BackendInfo
}
