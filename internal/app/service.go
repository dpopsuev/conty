package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	adapters  map[string]driven.CICore
	pipelines map[string]domain.Pipeline
	runs      map[string]*domain.PipelineRun
	owned     map[string]domain.OwnedRun
	mu        sync.RWMutex
}

func NewService(adapters ...driven.CICore) *Service {
	s := &Service{
		adapters:  make(map[string]driven.CICore, len(adapters)),
		pipelines: make(map[string]domain.Pipeline),
		runs:      make(map[string]*domain.PipelineRun),
		owned:     make(map[string]domain.OwnedRun),
	}
	for _, a := range adapters {
		s.adapters[a.Name()] = a
	}
	return s
}

func (s *Service) AddAdapter(a driven.CICore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adapters[a.Name()] = a
}

func (s *Service) RegisterPipeline(p domain.Pipeline) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pipelines[p.Name] = p
}

func (s *Service) adapter(name string) (driven.CICore, error) {
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

		var runID string
		if t, ok := a.(driven.CITriggerable); ok {
			receipt, triggerErr := t.Trigger(ctx, step.JobName, step.Params)
			if triggerErr != nil {
				run.Steps[i].Status = domain.RunStatusFailure
				run.Status = domain.RunStatusFailure
				s.storeRun(name, run)
				return run, nil
			}
			for receipt.NeedsResolve {
				time.Sleep(2 * time.Second)
				receipt, triggerErr = t.ResolveReceipt(ctx, receipt)
				if triggerErr != nil {
					run.Steps[i].Status = domain.RunStatusFailure
					run.Status = domain.RunStatusFailure
					s.storeRun(name, run)
					return run, nil
				}
			}
			runID = receipt.RunID
		} else {
			run.Steps[i].Status = domain.RunStatusFailure
			run.Status = domain.RunStatusFailure
			s.storeRun(name, run)
			return run, nil
		}
		run.Steps[i].RunID = runID

		ciRun, pollErr := a.GetRun(ctx, step.JobName, runID)
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

func (s *Service) GetStepLog(ctx context.Context, name string, step int, f domain.LogFilter) (domain.LogResult, error) {
	s.mu.RLock()
	p, ok := s.pipelines[name]
	run := s.runs[name]
	s.mu.RUnlock()
	if !ok || run == nil {
		return domain.LogResult{}, fmt.Errorf("%w: %s", ErrPipelineNotFound, name)
	}
	if step < 0 || step >= len(run.Steps) {
		return domain.LogResult{}, fmt.Errorf("%w: %d (pipeline has %d steps)", ErrStepOutOfRange, step, len(run.Steps))
	}

	a, err := s.adapter(p.Backend)
	if err != nil {
		return domain.LogResult{}, err
	}

	raw, err := a.GetLog(ctx, run.Steps[step].JobName, run.Steps[step].RunID, f)
	if err != nil {
		return domain.LogResult{}, err
	}
	return applyLogFilter(raw, f), nil
}

func (s *Service) ListBackends() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.adapters))
	for name := range s.adapters {
		names = append(names, name)
	}
	return names
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

func (s *Service) BackendInfo() []domain.BackendInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	infos := make([]domain.BackendInfo, 0, len(s.adapters))
	for _, a := range s.adapters {
		infos = append(infos, domain.BackendInfo{
			Name:         a.Name(),
			Type:         a.Type(),
			Capabilities: a.Capabilities().String(),
		})
	}
	return infos
}

func (s *Service) CIArtifacts(ctx context.Context, backend, jobRef, runID string) ([]domain.CIArtifact, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}
	st, ok := a.(driven.CIArtifactStore)
	if !ok {
		return nil, fmt.Errorf("backend %q does not support artifacts (capabilities: %v)", a.Name(), a.Capabilities())
	}
	return st.ListArtifacts(ctx, jobRef, runID)
}

func (s *Service) CIGetRun(ctx context.Context, backend, jobRef, runID string) (*domain.CIRun, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}
	return a.GetRun(ctx, jobRef, runID)
}

func (s *Service) CIDownstream(ctx context.Context, backend, downstreamJob, upstreamJob, upstreamRunID string) ([]domain.CIRun, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}
	c, ok := a.(driven.CIChainable)
	if !ok {
		return nil, fmt.Errorf("backend %q does not support chain traversal (capabilities: %v)", a.Name(), a.Capabilities())
	}
	return c.GetDownstreamRuns(ctx, downstreamJob, upstreamJob, upstreamRunID)
}

func (s *Service) CICancel(ctx context.Context, backend, jobRef, runID string) error {
	if !s.OwnsRun(backend, runID) {
		return fmt.Errorf("cannot cancel %s #%s: not owned by this session", jobRef, runID)
	}
	a, err := s.adapter(backend)
	if err != nil {
		return err
	}
	return a.CancelRun(ctx, jobRef, runID) // CancelRun is on CICore — no capability check needed
}

func (s *Service) CIArtifactGet(ctx context.Context, backend, jobRef, runID, path string) ([]byte, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}
	st, ok := a.(driven.CIArtifactStore)
	if !ok {
		return nil, fmt.Errorf("backend %q does not support artifact download (capabilities: %v)", a.Name(), a.Capabilities())
	}
	return st.GetArtifact(ctx, jobRef, runID, path)
}

// CIArtifactText fetches an artifact and applies LogFilter if the content is
// valid UTF-8 text. Binary artifacts return an error so callers can fall back
// to downloading the raw bytes via CIArtifactGet.
func (s *Service) CIArtifactText(ctx context.Context, backend, jobRef, runID, path string, f domain.LogFilter) (domain.LogResult, error) {
	data, err := s.CIArtifactGet(ctx, backend, jobRef, runID, path)
	if err != nil {
		return domain.LogResult{}, err
	}
	if !utf8.Valid(data) {
		return domain.LogResult{}, fmt.Errorf("artifact is binary; use ci_artifacts to get the download URL")
	}
	return applyLogFilter(string(data), f), nil
}

// CIParamsTruncated fetches build params and caps each value at maxValueLen
// characters to prevent large YAML/JSON blobs from flooding the context.
func (s *Service) CIParamsTruncated(ctx context.Context, backend, jobRef, runID string) (map[string]string, []string, error) {
	params, err := s.CIParams(ctx, backend, jobRef, runID)
	if err != nil {
		return nil, nil, err
	}
	const maxValueLen = 500
	var truncated []string
	for k, v := range params {
		if len(v) > maxValueLen {
			params[k] = v[:maxValueLen] + "..."
			truncated = append(truncated, k)
		}
	}
	return params, truncated, nil
}

func (s *Service) CheckLatest(ctx context.Context, backend, jobRef string) (*domain.CICheck, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}

	run, err := a.GetRun(ctx, jobRef, "latest")
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

func (s *Service) GetVerdict(ctx context.Context, backend, jobRef, runID string, f domain.LogFilter) (*domain.CIVerdict, error) {
	var check *domain.CICheck
	var err error
	if runID == "" {
		check, err = s.CheckLatest(ctx, backend, jobRef)
	} else {
		a, aerr := s.adapter(backend)
		if aerr != nil {
			return nil, aerr
		}
		run, rerr := a.GetRun(ctx, jobRef, runID)
		if rerr != nil {
			return nil, rerr
		}
		check = &domain.CICheck{
			JobRef:    jobRef,
			Backend:   backend,
			RunID:     run.ID,
			Status:    run.Status,
			CheckedAt: time.Now(),
		}
		err = nil
	}
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
		verdict.Failure = s.classifyFailure(ctx, a, jobRef, check.RunID, f)
	}

	return verdict, nil
}

func (s *Service) TriggerRedeploy(ctx context.Context, backend, jobRef string) (string, error) {
	return s.TriggerRedeployWithParams(ctx, backend, jobRef, nil)
}

func (s *Service) TriggerRedeployWithParams(ctx context.Context, backend, jobRef string, params map[string]string) (string, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return "", err
	}
	t, ok := a.(driven.CITriggerable)
	if !ok {
		return "", fmt.Errorf("backend %q does not support triggering (capabilities: %v)", a.Name(), a.Capabilities())
	}
	receipt, err := t.Trigger(ctx, jobRef, params)
	if err != nil {
		return "", err
	}
	if receipt.NeedsResolve {
		resolved, _ := t.ResolveReceipt(ctx, receipt)
		if resolved.RunID != "" {
			s.recordOwnership(backend, jobRef, resolved.RunID, resolved.OpaqueRef)
			return resolved.RunID, nil
		}
		s.recordOwnership(backend, jobRef, resolved.OpaqueRef, resolved.OpaqueRef)
		return resolved.OpaqueRef, nil
	}
	s.recordOwnership(backend, jobRef, receipt.RunID, receipt.OpaqueRef)
	return receipt.RunID, nil
}

func (s *Service) CITrigger(ctx context.Context, backend, jobRef string, params map[string]string) (*domain.TriggerResult, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}
	t, ok := a.(driven.CITriggerable)
	if !ok {
		return nil, fmt.Errorf("backend %q does not support triggering (capabilities: %v)", a.Name(), a.Capabilities())
	}

	receipt, err := t.Trigger(ctx, jobRef, params)
	if err != nil {
		return nil, err
	}
	// Try to resolve once — synchronous backends (GitLab) get RunID immediately;
	// async backends (Jenkins, GitHub) need the wait action to poll.
	if receipt.NeedsResolve {
		resolved, _ := t.ResolveReceipt(ctx, receipt)
		receipt = resolved
	}

	result := &domain.TriggerResult{
		QueueID: receipt.OpaqueRef, // backward compat: OpaqueRef exposed as queue_id
		JobRef:  jobRef,
		Backend: backend,
	}
	if receipt.RunID != "" {
		result.BuildNumber = receipt.RunID
		s.recordOwnership(backend, jobRef, receipt.RunID, receipt.OpaqueRef)
	}

	// Yellow: log estimated duration for progress tracking
	est, _ := t.EstimateDuration(ctx, jobRef)
	if est > 0 {
		result.EstimatedDuration = est
		interval := est / 20
		if interval < 60000 {
			interval = 60000
		}
		result.PollInterval = interval
	}

	return result, nil
}

func (s *Service) CIParams(ctx context.Context, backend, jobRef, runID string) (map[string]string, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}
	h, ok := a.(driven.CIHistorical)
	if !ok {
		return nil, fmt.Errorf("backend %q does not support run params (capabilities: %v)", a.Name(), a.Capabilities())
	}
	return h.GetRunParams(ctx, jobRef, runID)
}

func (s *Service) CIHistory(ctx context.Context, backend, jobRef string, limit int) ([]domain.CIRun, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	h, ok := a.(driven.CIHistorical)
	if !ok {
		return nil, fmt.Errorf("backend %q does not support run history (capabilities: %v)", a.Name(), a.Capabilities())
	}
	return h.ListRuns(ctx, jobRef, limit)
}

func (s *Service) CISearch(ctx context.Context, backend, jobRef string, f domain.BuildFilter) ([]domain.CIRun, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}
	return a.SearchRuns(ctx, jobRef, f)
}

func (s *Service) CILog(ctx context.Context, backend, jobRef, runID string, f domain.LogFilter) (domain.LogResult, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return domain.LogResult{}, err
	}
	if runID == "" {
		check, err := s.CheckLatest(ctx, backend, jobRef)
		if err != nil {
			return domain.LogResult{}, err
		}
		runID = check.RunID
	}
	raw, err := a.GetLog(ctx, jobRef, runID, f)
	if err != nil {
		return domain.LogResult{}, err
	}
	return applyLogFilter(raw, f), nil
}

func applyLogFilter(raw string, f domain.LogFilter) domain.LogResult {
	lines := strings.Split(raw, "\n")

	if f.Grep != "" {
		pattern := strings.ToLower(f.Grep)
		matched := lines[:0]
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l), pattern) {
				matched = append(matched, l)
			}
		}
		lines = matched
	}

	totalLines := len(lines)

	tail := f.Tail
	if tail == 0 {
		tail = domain.LogDefaultTail
	}

	skipped := 0
	truncated := false
	if tail > 0 && len(lines) > tail {
		skipped = len(lines) - tail
		lines = lines[skipped:]
		truncated = true
	}

	// Byte cap: if the tail still exceeds LogDefaultMaxBytes, trim from the front.
	byteTotal := 0
	for _, l := range lines {
		byteTotal += len(l) + 1
	}
	if byteTotal > domain.LogDefaultMaxBytes {
		for byteTotal > domain.LogDefaultMaxBytes && len(lines) > 0 {
			byteTotal -= len(lines[0]) + 1
			skipped++
			lines = lines[1:]
		}
		truncated = true
	}

	return domain.LogResult{
		Lines:      lines,
		TotalLines: totalLines,
		Skipped:    skipped,
		Filtered:   f.Grep != "",
		Truncated:  truncated,
	}
}

// CIPoll resolves an opaque trigger reference (formerly queue ID) to a run ID.
// Creates a TriggerReceipt from the opaque ref and calls ResolveReceipt once.
func (s *Service) CIPoll(ctx context.Context, backend, opaqueRef string) (string, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return "", err
	}
	t, ok := a.(driven.CITriggerable)
	if !ok {
		return "", fmt.Errorf("backend %q does not support trigger resolution", a.Name())
	}
	receipt := &domain.TriggerReceipt{OpaqueRef: opaqueRef, NeedsResolve: true, Backend: backend}
	resolved, err := t.ResolveReceipt(ctx, receipt)
	if err != nil {
		return "", err
	}
	return resolved.RunID, nil
}

func (s *Service) recordOwnership(backend, jobRef, buildNumber, queueID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := backend + ":" + buildNumber
	s.owned[key] = domain.OwnedRun{
		Backend:     backend,
		JobRef:      jobRef,
		BuildNumber: buildNumber,
		QueueID:     queueID,
	}
}

func (s *Service) OwnsRun(backend, buildNumber string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.owned[backend+":"+buildNumber]
	return ok
}

func (s *Service) ListOwnedRuns() []domain.OwnedRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]domain.OwnedRun, 0, len(s.owned))
	for _, r := range s.owned {
		runs = append(runs, r)
	}
	return runs
}

func (s *Service) CIWatch(ctx context.Context, backend, jobRef, runID string) (*domain.WatchStatus, error) {
	a, err := s.adapter(backend)
	if err != nil {
		return nil, err
	}

	run, err := a.GetRun(ctx, jobRef, runID)
	if err != nil {
		return nil, err
	}

	var estimated int64
	if t, ok := a.(driven.CITriggerable); ok {
		estimated, _ = t.EstimateDuration(ctx, jobRef)
	}

	ws := &domain.WatchStatus{
		BuildNumber: run.ID,
		JobRef:      jobRef,
		Backend:     backend,
		Status:      run.Status,
		Elapsed:     run.Duration,
		Estimated:   estimated,
	}

	if estimated > 0 {
		ws.Progress = float64(run.Duration) / float64(estimated) * 100
		ws.Overdue = float64(run.Duration) > float64(estimated)*1.5
	}

	return ws, nil
}

func (s *Service) classifyFailure(ctx context.Context, a driven.CICore, jobRef, runID string, f domain.LogFilter) *domain.FailureContext {
	fc := &domain.FailureContext{
		Classification: domain.FailureUnknown,
		CanRetry:       false,
	}

	if p, ok := a.(driven.CIPipeliner); ok {
		if stages, err := p.ListStages(ctx, jobRef, runID); err == nil {
			for _, j := range stages {
				if j.Status == domain.RunStatusFailure {
					fc.FailedJob = j.Name
					break
				}
			}
		}
	}

	raw, err := a.GetLog(ctx, jobRef, runID, f)
	if err == nil && len(raw) > 0 {
		fc.Log = applyLogFilter(raw, f)
		fc.Classification, fc.CanRetry = classifyLog(strings.Join(fc.Log.Lines, "\n"))
	}

	return fc
}

func (s *Service) storeRun(name string, run *domain.PipelineRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[name] = run
}
