package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

var (
	ErrBackendNotFound  = errors.New("backend not found")
	ErrPipelineNotFound = errors.New("pipeline not found")
	ErrStepOutOfRange   = errors.New("step index out of range")
	ErrCannotRedeploy   = errors.New("cannot redeploy: failure is not transient")
)

type Service struct {
	adapters  map[string]driven.CIAdapter
	pipelines map[string]domain.Pipeline
	runs      map[string]*domain.PipelineRun
	mu        sync.RWMutex
}

func NewService(adapters ...driven.CIAdapter) *Service {
	s := &Service{
		adapters:  make(map[string]driven.CIAdapter, len(adapters)),
		pipelines: make(map[string]domain.Pipeline),
		runs:      make(map[string]*domain.PipelineRun),
	}
	for _, a := range adapters {
		s.adapters[a.Name()] = a
	}
	return s
}

func (s *Service) AddAdapter(a driven.CIAdapter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adapters[a.Name()] = a
}

func (s *Service) RegisterPipeline(p domain.Pipeline) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pipelines[p.Name] = p
}

func (s *Service) adapter(name string) (driven.CIAdapter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.adapters[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrBackendNotFound, name)
	}
	return a, nil
}

func (s *Service) TriggerPipeline(ctx context.Context, name string) (*domain.PipelineRun, error) {
	s.mu.RLock()
	p, ok := s.pipelines[name]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPipelineNotFound, name)
	}

	a, err := s.adapter(p.Backend)
	if err != nil {
		return nil, err
	}

	run := &domain.PipelineRun{
		Pipeline:  name,
		Status:    domain.RunStatusRunning,
		Steps:     make([]domain.StepResult, len(p.Steps)),
		StartedAt: time.Now(),
	}

	for i, step := range p.Steps {
		run.Steps[i] = domain.StepResult{
			JobName:   step.JobName,
			Status:    domain.RunStatusRunning,
			StartedAt: time.Now(),
		}

		runID, triggerErr := a.TriggerRun(ctx, step.JobName, step.Params)
		if triggerErr != nil {
			run.Steps[i].Status = domain.RunStatusFailure
			run.Status = domain.RunStatusFailure
			s.storeRun(name, run)
			return run, nil
		}
		run.Steps[i].RunID = runID

		ciRun, pollErr := a.PollRun(ctx, step.JobName, runID)
		if pollErr != nil {
			run.Steps[i].Status = domain.RunStatusFailure
			run.Status = domain.RunStatusFailure
			s.storeRun(name, run)
			return run, nil
		}

		run.Steps[i].Status = ciRun.Status
		run.Steps[i].Result = ciRun.Result
		run.Steps[i].Duration = ciRun.Duration
		run.Steps[i].URL = ciRun.URL

		if ciRun.Status != domain.RunStatusSuccess {
			run.Status = domain.RunStatusFailure
			s.storeRun(name, run)
			return run, nil
		}
	}

	run.Status = domain.RunStatusSuccess
	run.Duration = time.Since(run.StartedAt).Milliseconds()
	s.storeRun(name, run)
	return run, nil
}

func (s *Service) GetPipelineStatus(_ context.Context, name string) (*domain.PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPipelineNotFound, name)
	}
	return run, nil
}

func (s *Service) GetStepLog(ctx context.Context, name string, step int) (string, error) {
	s.mu.RLock()
	p, ok := s.pipelines[name]
	run := s.runs[name]
	s.mu.RUnlock()
	if !ok || run == nil {
		return "", fmt.Errorf("%w: %s", ErrPipelineNotFound, name)
	}
	if step < 0 || step >= len(run.Steps) {
		return "", fmt.Errorf("%w: %d (pipeline has %d steps)", ErrStepOutOfRange, step, len(run.Steps))
	}

	a, err := s.adapter(p.Backend)
	if err != nil {
		return "", err
	}

	return a.GetJobLog(ctx, run.Steps[step].JobName, run.Steps[step].RunID)
}

func (s *Service) ListPipelines() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.pipelines))
	for name := range s.pipelines {
		names = append(names, name)
	}
	return names
}

func (s *Service) CheckLatest(ctx context.Context, backend, jobRef string) (*domain.CICheck, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}

	run, err := a.PollRun(ctx, jobRef, "latest")
	if err != nil {
		return nil, err
	}

	return &domain.CICheck{
		JobRef:    jobRef,
		Backend:   backend,
		RunID:     run.ID,
		Status:    run.Status,
		CheckedAt: time.Now(),
	}, nil
}

func (s *Service) GetVerdict(ctx context.Context, backend, jobRef string) (*domain.CIVerdict, error) {
	check, err := s.CheckLatest(ctx, backend, jobRef)
	if err != nil {
		return nil, err
	}

	verdict := &domain.CIVerdict{Check: *check}

	if check.Status == domain.RunStatusSuccess {
		verdict.TestSummary = &domain.TestSummary{}
		return verdict, nil
	}

	if check.Status == domain.RunStatusFailure {
		a, _ := s.adapter(backend)
		verdict.Failure = s.classifyFailure(ctx, a, jobRef, check.RunID)
	}

	return verdict, nil
}

func (s *Service) TriggerRedeploy(ctx context.Context, backend, jobRef string) (string, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return "", err
	}
	return a.TriggerRun(ctx, jobRef, nil)
}

func (s *Service) classifyFailure(ctx context.Context, a driven.CIAdapter, jobRef, runID string) *domain.FailureContext {
	fc := &domain.FailureContext{
		Classification: domain.FailureUnknown,
		CanRetry:       false,
	}

	jobs, err := a.ListJobs(ctx, jobRef, runID)
	if err == nil {
		for _, j := range jobs {
			if j.Status == domain.RunStatusFailure {
				fc.FailedJob = j.Name
				break
			}
		}
	}

	log, err := a.GetJobLog(ctx, jobRef, runID)
	if err == nil && len(log) > 0 {
		maxLen := 2000
		if len(log) < maxLen {
			maxLen = len(log)
		}
		fc.LogExcerpt = log[len(log)-maxLen:]
	}

	return fc
}

func (s *Service) storeRun(name string, run *domain.PipelineRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[name] = run
}
