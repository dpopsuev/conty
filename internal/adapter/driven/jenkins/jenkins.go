package jenkins

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	adapterdriven "github.com/dpopsuev/conty/internal/adapter/driven"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
	"github.com/bndr/gojenkins"
)

const BackendName = "jenkins"

var _ driven.CIAdapter = (*Adapter)(nil)

var (
	ErrJobNotFound      = errors.New("job not found")
	ErrBuildNotFound    = errors.New("build not found")
	ErrArtifactNotFound = errors.New("artifact not found")
)

type Adapter struct {
	name    string
	jenkins *gojenkins.Jenkins
	baseURL string
	user    string
	token   string
}

func New(ctx context.Context, name, baseURL, user, token string) (*Adapter, error) {
	j, err := gojenkins.CreateJenkins(nil, baseURL, user, token).Init(ctx)
	if err != nil {
		return nil, fmt.Errorf("jenkins init: %w", err)
	}
	return &Adapter{name: name, jenkins: j, baseURL: baseURL, user: user, token: token}, nil
}

func (a *Adapter) Name() string { return a.name }

func (a *Adapter) TriggerRun(ctx context.Context, jobName string, params map[string]string) (string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "trigger_run", slog.String(adapterdriven.LogKeyID, jobName))

	queueID, err := a.jenkins.BuildJob(ctx, jobName, params)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "trigger_run", err)
		return "", err
	}

	adapterdriven.LogOpDone(ctx, a.name, "trigger_run",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int64("queue_id", queueID))
	return strconv.FormatInt(queueID, 10), nil
}

func (a *Adapter) PollRun(ctx context.Context, jobName string, runID string) (*domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "poll_run",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "poll_run", err)
		return nil, err
	}

	var b *gojenkins.Build
	if runID == "latest" {
		b, err = j.GetLastBuild(ctx)
	} else {
		num, parseErr := strconv.ParseInt(runID, 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid run ID %q: %w", runID, parseErr)
		}
		b, err = j.GetBuild(ctx, num)
	}
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "poll_run", err)
		return nil, fmt.Errorf("%w: %s #%s", ErrBuildNotFound, jobName, runID)
	}

	run := a.mapBuild(ctx, b)
	adapterdriven.LogOpDone(ctx, a.name, "poll_run",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return run, nil
}

func (a *Adapter) ListJobs(ctx context.Context, jobName string, runID string) ([]domain.CIJob, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_jobs",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_jobs", err)
		return nil, err
	}

	raw, err := j.GetPipelineRun(ctx, runID)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_jobs", err)
		return nil, err
	}

	jobs := make([]domain.CIJob, 0, len(raw.Stages))
	for i := range raw.Stages {
		jobs = append(jobs, domain.CIJob{
			ID:        raw.Stages[i].ID,
			Name:      raw.Stages[i].Name,
			Status:    mapPipelineStatus(raw.Stages[i].Status),
			ParentRun: runID,
			StartedAt: time.UnixMilli(raw.Stages[i].StartTime),
			Duration:  int64(raw.Stages[i].Duration),
		})
	}

	adapterdriven.LogOpDone(ctx, a.name, "list_jobs",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(jobs)))
	return jobs, nil
}

func (a *Adapter) GetJobLog(ctx context.Context, jobName string, runID string) (string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_job_log",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_job_log", err)
		return "", err
	}

	num, parseErr := strconv.ParseInt(runID, 10, 64)
	if parseErr != nil {
		return "", fmt.Errorf("invalid run ID %q: %w", runID, parseErr)
	}

	b, err := j.GetBuild(ctx, num)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_job_log", err)
		return "", fmt.Errorf("%w: %s #%s", ErrBuildNotFound, jobName, runID)
	}

	output := b.GetConsoleOutput(ctx)
	adapterdriven.LogOpDone(ctx, a.name, "get_job_log",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return output, nil
}

func (a *Adapter) ListArtifacts(ctx context.Context, jobName string, runID string) ([]domain.CIArtifact, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_artifacts",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_artifacts", err)
		return nil, err
	}

	num, parseErr := strconv.ParseInt(runID, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("invalid run ID %q: %w", runID, parseErr)
	}

	b, err := j.GetBuild(ctx, num)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_artifacts", err)
		return nil, fmt.Errorf("%w: %s #%s", ErrBuildNotFound, jobName, runID)
	}

	raw := b.GetArtifacts()
	artifacts := make([]domain.CIArtifact, 0, len(raw))
	for i := range raw {
		artifacts = append(artifacts, domain.CIArtifact{
			Name: raw[i].FileName,
			Path: raw[i].Path,
		})
	}

	adapterdriven.LogOpDone(ctx, a.name, "list_artifacts",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(artifacts)))
	return artifacts, nil
}

func (a *Adapter) GetArtifact(ctx context.Context, jobName string, runID string, path string) ([]byte, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_artifact",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID),
		slog.String("path", path))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_artifact", err)
		return nil, err
	}

	num, parseErr := strconv.ParseInt(runID, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("invalid run ID %q: %w", runID, parseErr)
	}

	b, err := j.GetBuild(ctx, num)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_artifact", err)
		return nil, fmt.Errorf("%w: %s #%s", ErrBuildNotFound, jobName, runID)
	}

	url := b.GetUrl() + "artifact/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(a.user, a.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_artifact", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrArtifactNotFound, path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artifact download failed: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	adapterdriven.LogOpDone(ctx, a.name, "get_artifact",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int("size", len(data)))
	return data, nil
}

func (a *Adapter) GetEstimatedDuration(ctx context.Context, jobName string) (int64, error) {
	j, err := a.getJob(ctx, jobName)
	if err != nil {
		return 0, err
	}
	b, err := j.GetLastBuild(ctx)
	if err != nil {
		return 0, nil
	}
	return int64(b.Raw.EstimatedDuration), nil
}

func (a *Adapter) PollQueue(ctx context.Context, queueID string) (string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "poll_queue", slog.String("queue_id", queueID))

	id, err := strconv.ParseInt(queueID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid queue ID %q: %w", queueID, err)
	}

	task, err := a.jenkins.GetQueueItem(ctx, id)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "poll_queue", err)
		return "", err
	}

	buildNum := task.Raw.Executable.Number
	if buildNum == 0 {
		adapterdriven.LogOpDone(ctx, a.name, "poll_queue",
			slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
			slog.String("status", "queued"))
		return "", nil
	}

	result := strconv.FormatInt(buildNum, 10)
	adapterdriven.LogOpDone(ctx, a.name, "poll_queue",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.String("build_number", result))
	return result, nil
}

func (a *Adapter) GetBuildParams(ctx context.Context, jobName string, runID string) (map[string]string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_build_params",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		return nil, err
	}

	num, err := strconv.ParseInt(runID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid run ID %q: %w", runID, err)
	}

	b, err := j.GetBuild(ctx, num)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_build_params", err)
		return nil, fmt.Errorf("%w: %s #%s", ErrBuildNotFound, jobName, runID)
	}

	params := make(map[string]string)
	for _, p := range b.GetParameters() {
		params[p.Name] = fmt.Sprintf("%v", p.Value)
	}

	adapterdriven.LogOpDone(ctx, a.name, "get_build_params",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(params)))
	return params, nil
}

func (a *Adapter) ListBuilds(ctx context.Context, jobName string, limit int) ([]domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_builds",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.Int("limit", limit))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		return nil, err
	}

	buildIDs, err := j.GetAllBuildIds(ctx)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_builds", err)
		return nil, err
	}
	if limit > 0 && len(buildIDs) > limit {
		buildIDs = buildIDs[:limit]
	}

	runs := make([]domain.CIRun, 0, len(buildIDs))
	for _, bid := range buildIDs {
		b, err := j.GetBuild(ctx, bid.Number)
		if err != nil {
			continue
		}
		runs = append(runs, *a.mapBuild(ctx, b))
	}

	adapterdriven.LogOpDone(ctx, a.name, "list_builds",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(runs)))
	return runs, nil
}

func (a *Adapter) getJob(ctx context.Context, name string) (*gojenkins.Job, error) {
	parts := strings.Split(name, "/")
	id := parts[len(parts)-1]
	var j *gojenkins.Job
	var err error
	if len(parts) > 1 {
		j, err = a.jenkins.GetJob(ctx, id, parts[:len(parts)-1]...)
	} else {
		j, err = a.jenkins.GetJob(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, name)
	}
	return j, nil
}

func (a *Adapter) mapBuild(ctx context.Context, b *gojenkins.Build) *domain.CIRun {
	status := domain.RunStatusSuccess
	if b.IsRunning(ctx) {
		status = domain.RunStatusRunning
	} else if b.GetResult() == "FAILURE" {
		status = domain.RunStatusFailure
	} else if b.GetResult() == "ABORTED" {
		status = domain.RunStatusAborted
	}

	return &domain.CIRun{
		ID:        strconv.FormatInt(b.GetBuildNumber(), 10),
		Name:      b.Raw.FullDisplayName,
		Status:    status,
		Result:    domain.RunResult(b.GetResult()),
		URL:       b.GetUrl(),
		StartedAt: b.GetTimestamp(),
		Duration:  int64(b.GetDuration()),
	}
}

func mapPipelineStatus(status string) domain.RunStatus {
	switch strings.ToUpper(status) {
	case "SUCCESS":
		return domain.RunStatusSuccess
	case "IN_PROGRESS", "RUNNING":
		return domain.RunStatusRunning
	case "FAILED", "FAILURE":
		return domain.RunStatusFailure
	case "ABORTED":
		return domain.RunStatusAborted
	default:
		return domain.RunStatusPending
	}
}
