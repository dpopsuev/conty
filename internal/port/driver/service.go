package driver

import (
	"context"

	"github.com/dpopsuev/conty/internal/domain"
)

type PipelineService interface {
	TriggerPipeline(ctx context.Context, name string) (*domain.PipelineRun, error)
	GetPipelineStatus(ctx context.Context, name string) (*domain.PipelineRun, error)
	GetStepLog(ctx context.Context, name string, step int) (string, error)
	ListPipelines() []string
	ListBackends() []string
}

type CIMonitorService interface {
	CheckLatest(ctx context.Context, backend, jobRef string) (*domain.CICheck, error)
	GetVerdict(ctx context.Context, backend, jobRef string) (*domain.CIVerdict, error)
	TriggerRedeploy(ctx context.Context, backend, jobRef string) (string, error)
	TriggerRedeployWithParams(ctx context.Context, backend, jobRef string, params map[string]string) (string, error)
}
