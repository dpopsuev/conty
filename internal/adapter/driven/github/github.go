package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	adapterdriven "github.com/dpopsuev/conty/internal/adapter/driven"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

const (
	apiURL      = "https://api.github.com"
	BackendName = "github"
)

var _ driven.CIAdapter = (*Adapter)(nil)

var (
	ErrOwnerRequired = errors.New("repository owner is required")
	ErrRepoRequired  = errors.New("repo not set")
	ErrAuthRequired  = errors.New("write operation requires token")
	ErrAPIError      = errors.New("github API error")
	ErrNotFound      = errors.New("not found")
)

type Adapter struct {
	name    string
	baseURL string
	token   string
	owner   string
	repo    string
	client  *http.Client
}

func New(name, token, owner, repo string) (*Adapter, error) {
	if owner == "" {
		return nil, ErrOwnerRequired
	}
	if repo == "" {
		return nil, ErrRepoRequired
	}
	return &Adapter{
		name:    name,
		baseURL: apiURL,
		token:   token,
		owner:   owner,
		repo:    repo,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (a *Adapter) Name() string { return a.name }

func (a *Adapter) TriggerRun(ctx context.Context, jobName string, params map[string]string) (string, error) {
	if a.token == "" {
		return "", ErrAuthRequired
	}
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "trigger_run", slog.String(adapterdriven.LogKeyID, jobName))

	body := map[string]any{"ref": "main"}
	if params != nil {
		inputs := make(map[string]string, len(params))
		for k, v := range params {
			inputs[k] = v
		}
		body["inputs"] = inputs
		if ref, ok := params["ref"]; ok {
			body["ref"] = ref
		}
	}

	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches", a.owner, a.repo, jobName)
	if err := a.api(ctx, http.MethodPost, path, body, nil); err != nil {
		adapterdriven.LogError(ctx, a.name, "trigger_run", err)
		return "", err
	}

	adapterdriven.LogOpDone(ctx, a.name, "trigger_run",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return "dispatched", nil
}

func (a *Adapter) PollRun(ctx context.Context, jobName string, runID string) (*domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "poll_run",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	var path string
	if runID == "latest" {
		path = fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=1", a.owner, a.repo)
		var resp struct {
			WorkflowRuns []ghWorkflowRun `json:"workflow_runs"`
		}
		if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
			adapterdriven.LogError(ctx, a.name, "poll_run", err)
			return nil, err
		}
		if len(resp.WorkflowRuns) == 0 {
			return nil, fmt.Errorf("%w: no runs found", ErrNotFound)
		}
		run := resp.WorkflowRuns[0].toCIRun()
		adapterdriven.LogOpDone(ctx, a.name, "poll_run",
			slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
		return &run, nil
	}

	path = fmt.Sprintf("/repos/%s/%s/actions/runs/%s", a.owner, a.repo, runID)
	var resp ghWorkflowRun
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "poll_run", err)
		return nil, err
	}
	run := resp.toCIRun()
	adapterdriven.LogOpDone(ctx, a.name, "poll_run",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return &run, nil
}

func (a *Adapter) ListJobs(ctx context.Context, _ string, runID string) ([]domain.CIJob, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_jobs", slog.String("run_id", runID))

	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%s/jobs", a.owner, a.repo, runID)
	var resp struct {
		Jobs []ghWorkflowJob `json:"jobs"`
	}
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "list_jobs", err)
		return nil, err
	}

	jobs := make([]domain.CIJob, len(resp.Jobs))
	for i := range resp.Jobs {
		jobs[i] = resp.Jobs[i].toCIJob()
	}
	adapterdriven.LogOpDone(ctx, a.name, "list_jobs",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(jobs)))
	return jobs, nil
}

func (a *Adapter) GetJobLog(ctx context.Context, _ string, runID string) (string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_job_log", slog.String("run_id", runID))

	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%s/logs", a.baseURL, a.owner, a.repo, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "token "+a.token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := a.client.Do(req)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_job_log", err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%w: GET run logs: %d: %s", ErrAPIError, resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read logs: %w", err)
	}
	adapterdriven.LogOpDone(ctx, a.name, "get_job_log",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return string(data), nil
}

func (a *Adapter) ListArtifacts(ctx context.Context, _ string, runID string) ([]domain.CIArtifact, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_artifacts", slog.String("run_id", runID))

	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%s/artifacts", a.owner, a.repo, runID)
	var resp struct {
		Artifacts []ghArtifact `json:"artifacts"`
	}
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "list_artifacts", err)
		return nil, err
	}

	artifacts := make([]domain.CIArtifact, len(resp.Artifacts))
	for i := range resp.Artifacts {
		artifacts[i] = domain.CIArtifact{
			Name: resp.Artifacts[i].Name,
			Path: strconv.FormatInt(resp.Artifacts[i].ID, 10),
			Size: resp.Artifacts[i].SizeInBytes,
		}
	}
	adapterdriven.LogOpDone(ctx, a.name, "list_artifacts",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(artifacts)))
	return artifacts, nil
}

func (a *Adapter) GetArtifact(ctx context.Context, _ string, _ string, path string) ([]byte, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_artifact", slog.String("path", path))

	url := fmt.Sprintf("%s/repos/%s/%s/actions/artifacts/%s/zip", a.baseURL, a.owner, a.repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "token "+a.token)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_artifact", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: artifact %s", ErrNotFound, path)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: GET artifact: %d", ErrAPIError, resp.StatusCode)
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

func (a *Adapter) GetEstimatedDuration(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (a *Adapter) PollQueue(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: GitHub Actions uses run IDs, not queue IDs", ErrNotFound)
}

func (a *Adapter) GetBuildParams(_ context.Context, _, _ string) (map[string]string, error) {
	return nil, fmt.Errorf("%w: GitHub Actions workflow inputs not yet supported", ErrNotFound)
}

func (a *Adapter) ListBuilds(ctx context.Context, _ string, limit int) ([]domain.CIRun, error) {
	if limit <= 0 {
		limit = 10
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=%d", a.owner, a.repo, limit)
	var resp struct {
		WorkflowRuns []ghWorkflowRun `json:"workflow_runs"`
	}
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	runs := make([]domain.CIRun, len(resp.WorkflowRuns))
	for i := range resp.WorkflowRuns {
		runs[i] = resp.WorkflowRuns[i].toCIRun()
	}
	return runs, nil
}

func (a *Adapter) api(ctx context.Context, method, path string, body, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "token "+a.token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		limit, remaining, reset := adapterdriven.ParseRateLimitHeaders(resp.Header)
		return &adapterdriven.RateLimitError{
			Backend:    a.name,
			RetryAfter: adapterdriven.ParseRetryAfter(resp.Header.Get("Retry-After")),
			Limit:      limit,
			Remaining:  remaining,
			Reset:      reset,
			Message:    string(respBody),
		}
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%w: %s %s: %d: %s", ErrAPIError, method, path, resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}
	return nil
}

type ghWorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadBranch string `json:"head_branch"`
	HTMLURL    string `json:"html_url"`
	CreatedAt  string `json:"created_at"`
}

func (r ghWorkflowRun) toCIRun() domain.CIRun {
	created, _ := time.Parse(time.RFC3339, r.CreatedAt)
	return domain.CIRun{
		ID:        strconv.FormatInt(r.ID, 10),
		Name:      r.Name,
		Status:    mapGHStatus(r.Status, r.Conclusion),
		Result:    mapGHResult(r.Conclusion),
		URL:       r.HTMLURL,
		StartedAt: created,
	}
}

type ghWorkflowJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	StartedAt  string `json:"started_at"`
}

func (j ghWorkflowJob) toCIJob() domain.CIJob {
	started, _ := time.Parse(time.RFC3339, j.StartedAt)
	return domain.CIJob{
		ID:        strconv.FormatInt(j.ID, 10),
		Name:      j.Name,
		Status:    mapGHStatus(j.Status, j.Conclusion),
		Result:    mapGHResult(j.Conclusion),
		StartedAt: started,
	}
}

type ghArtifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SizeInBytes int64  `json:"size_in_bytes"`
}

func mapGHStatus(status, conclusion string) domain.RunStatus {
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			return domain.RunStatusSuccess
		case "failure":
			return domain.RunStatusFailure
		case "cancelled":
			return domain.RunStatusAborted
		default:
			return domain.RunStatusFailure
		}
	case "in_progress", "queued":
		return domain.RunStatusRunning
	default:
		return domain.RunStatusPending
	}
}

func mapGHResult(conclusion string) domain.RunResult {
	switch conclusion {
	case "success":
		return domain.RunResultSuccess
	case "failure":
		return domain.RunResultFailure
	case "cancelled":
		return domain.RunResultAborted
	default:
		return ""
	}
}
