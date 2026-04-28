package driven

import (
	"context"

	"github.com/dpopsuev/conty/internal/domain"
)

type CIAdapter interface {
	Name() string
	TriggerRun(ctx context.Context, jobName string, params map[string]string) (string, error)
	PollRun(ctx context.Context, jobName string, runID string) (*domain.CIRun, error)
	PollQueue(ctx context.Context, queueID string) (string, error)
	ListJobs(ctx context.Context, jobName string, runID string) ([]domain.CIJob, error)
	GetJobLog(ctx context.Context, jobName string, runID string) (string, error)
	GetBuildParams(ctx context.Context, jobName string, runID string) (map[string]string, error)
	ListBuilds(ctx context.Context, jobName string, limit int) ([]domain.CIRun, error)
	ListArtifacts(ctx context.Context, jobName string, runID string) ([]domain.CIArtifact, error)
	GetArtifact(ctx context.Context, jobName string, runID string, path string) ([]byte, error)
	GetEstimatedDuration(ctx context.Context, jobName string) (int64, error)
}
