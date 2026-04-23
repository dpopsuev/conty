package driven

import (
	"context"

	"github.com/DanyPops/conty/internal/domain"
)

type CIAdapter interface {
	Name() string
	TriggerRun(ctx context.Context, jobName string, params map[string]string) (string, error)
	PollRun(ctx context.Context, jobName string, runID string) (*domain.CIRun, error)
	ListJobs(ctx context.Context, jobName string, runID string) ([]domain.CIJob, error)
	GetJobLog(ctx context.Context, jobName string, runID string) (string, error)
	ListArtifacts(ctx context.Context, jobName string, runID string) ([]domain.CIArtifact, error)
	GetArtifact(ctx context.Context, jobName string, runID string, path string) ([]byte, error)
}
