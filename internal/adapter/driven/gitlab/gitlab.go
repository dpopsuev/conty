package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	adapterdriven "github.com/dpopsuev/conty/internal/adapter/driven"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

const (
	defaultURL  = "https://gitlab.com"
	BackendName = "gitlab"
)

var _ driven.CIAdapter = (*Adapter)(nil)

var (
	ErrProjectRequired = errors.New("project ID is required")
	ErrAuthRequired    = errors.New("write operation requires token")
	ErrAPIError        = errors.New("gitlab API error")
	ErrNotFound        = errors.New("not found")
)

type Adapter struct {
	name      string
	baseURL   string
	token     string
	projectID string
	client    *http.Client
}

func New(name, token, projectID, baseURL string) (*Adapter, error) {
	if projectID == "" {
		return nil, ErrProjectRequired
	}
	if baseURL == "" {
		baseURL = defaultURL
	}
	return &Adapter{
		name:      name,
		baseURL:   strings.TrimRight(baseURL, "/"),
		token:     token,
		projectID: url.PathEscape(projectID),
		client:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (a *Adapter) Name() string { return a.name }

func (a *Adapter) TriggerRun(ctx context.Context, jobName string, params map[string]string) (string, error) {
	if a.token == "" {
		return "", ErrAuthRequired
	}
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "trigger_run", slog.String(adapterdriven.LogKeyID, jobName))

	ref := "main"
	if r, ok := params["ref"]; ok {
		ref = r
	}

	path := fmt.Sprintf("/api/v4/projects/%s/pipeline?ref=%s", a.projectID, ref)
	var resp glPipeline
	if err := a.api(ctx, http.MethodPost, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "trigger_run", err)
		return "", err
	}

	adapterdriven.LogOpDone(ctx, a.name, "trigger_run",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return strconv.FormatInt(resp.ID, 10), nil
}

func (a *Adapter) PollRun(ctx context.Context, _ string, runID string) (*domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "poll_run", slog.String("run_id", runID))

	var path string
	if runID == "latest" {
		path = fmt.Sprintf("/api/v4/projects/%s/pipelines?per_page=1", a.projectID)
		var resp []glPipeline
		if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
			adapterdriven.LogError(ctx, a.name, "poll_run", err)
			return nil, err
		}
		if len(resp) == 0 {
			return nil, fmt.Errorf("%w: no pipelines found", ErrNotFound)
		}
		run := resp[0].toCIRun()
		adapterdriven.LogOpDone(ctx, a.name, "poll_run",
			slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
		return &run, nil
	}

	path = fmt.Sprintf("/api/v4/projects/%s/pipelines/%s", a.projectID, runID)
	var resp glPipeline
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

	path := fmt.Sprintf("/api/v4/projects/%s/pipelines/%s/jobs?per_page=100", a.projectID, runID)
	var resp []glJob
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "list_jobs", err)
		return nil, err
	}

	jobs := make([]domain.CIJob, len(resp))
	for i := range resp {
		jobs[i] = resp[i].toCIJob()
	}
	adapterdriven.LogOpDone(ctx, a.name, "list_jobs",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(jobs)))
	return jobs, nil
}

func (a *Adapter) GetJobLog(ctx context.Context, _ string, runID string) (string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_job_log", slog.String("run_id", runID))

	fullURL := fmt.Sprintf("%s/api/v4/projects/%s/jobs/%s/trace", a.baseURL, a.projectID, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		return "", err
	}
	if a.token != "" {
		req.Header.Set("PRIVATE-TOKEN", a.token)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_job_log", err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%w: GET job trace: %d: %s", ErrAPIError, resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read trace: %w", err)
	}
	adapterdriven.LogOpDone(ctx, a.name, "get_job_log",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return string(data), nil
}

func (a *Adapter) ListArtifacts(ctx context.Context, _ string, runID string) ([]domain.CIArtifact, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_artifacts", slog.String("run_id", runID))

	path := fmt.Sprintf("/api/v4/projects/%s/jobs/%s/artifacts", a.projectID, runID)
	var resp []glArtifact
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		if errors.Is(err, ErrNotFound) {
			adapterdriven.LogOpDone(ctx, a.name, "list_artifacts",
				slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
				slog.Int(adapterdriven.LogKeyCount, 0))
			return nil, nil
		}
		adapterdriven.LogError(ctx, a.name, "list_artifacts", err)
		return nil, err
	}

	artifacts := make([]domain.CIArtifact, len(resp))
	for i := range resp {
		artifacts[i] = domain.CIArtifact{
			Name: resp[i].Filename,
			Path: resp[i].Filename,
			Size: resp[i].Size,
		}
	}
	adapterdriven.LogOpDone(ctx, a.name, "list_artifacts",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(artifacts)))
	return artifacts, nil
}

func (a *Adapter) GetArtifact(ctx context.Context, _ string, runID string, path string) ([]byte, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_artifact",
		slog.String("run_id", runID), slog.String("path", path))

	fullURL := fmt.Sprintf("%s/api/v4/projects/%s/jobs/%s/artifacts/%s",
		a.baseURL, a.projectID, runID, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	if a.token != "" {
		req.Header.Set("PRIVATE-TOKEN", a.token)
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

func (a *Adapter) PollQueue(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: GitLab CI uses pipeline IDs, not queue IDs", ErrNotFound)
}

func (a *Adapter) GetBuildParams(_ context.Context, _, _ string) (map[string]string, error) {
	return nil, fmt.Errorf("%w: GitLab CI pipeline variables not yet supported", ErrNotFound)
}

func (a *Adapter) ListBuilds(ctx context.Context, _ string, limit int) ([]domain.CIRun, error) {
	if limit <= 0 {
		limit = 10
	}
	path := fmt.Sprintf("/api/v4/projects/%s/pipelines?per_page=%d", a.projectID, limit)
	var resp []glPipeline
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	runs := make([]domain.CIRun, len(resp))
	for i := range resp {
		runs[i] = resp[i].toCIRun()
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
		bodyReader = strings.NewReader(string(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if a.token != "" {
		req.Header.Set("PRIVATE-TOKEN", a.token)
	}
	req.Header.Set("Content-Type", "application/json")

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

type glPipeline struct {
	ID        int64   `json:"id"`
	Status    string  `json:"status"`
	Ref       string  `json:"ref"`
	SHA       string  `json:"sha"`
	WebURL    string  `json:"web_url"`
	CreatedAt string  `json:"created_at"`
	Duration  float64 `json:"duration"`
}

func (p glPipeline) toCIRun() domain.CIRun {
	created, _ := time.Parse(time.RFC3339, p.CreatedAt)
	return domain.CIRun{
		ID:        strconv.FormatInt(p.ID, 10),
		Name:      p.Ref,
		Status:    mapGLStatus(p.Status),
		Result:    mapGLResult(p.Status),
		URL:       p.WebURL,
		StartedAt: created,
		Duration:  int64(p.Duration),
	}
}

type glJob struct {
	ID     int64 `json:"id"`
	Name   string `json:"name"`
	Stage  string `json:"stage"`
	Status string `json:"status"`
	WebURL string `json:"web_url"`
	StartedAt string  `json:"started_at"`
	Duration  float64 `json:"duration"`
}

func (j glJob) toCIJob() domain.CIJob {
	started, _ := time.Parse(time.RFC3339, j.StartedAt)
	return domain.CIJob{
		ID:        strconv.FormatInt(j.ID, 10),
		Name:      j.Name,
		Status:    mapGLStatus(j.Status),
		Result:    mapGLResult(j.Status),
		StartedAt: started,
		Duration:  int64(j.Duration),
	}
}

type glArtifact struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

func mapGLStatus(status string) domain.RunStatus {
	switch status {
	case "success":
		return domain.RunStatusSuccess
	case "failed":
		return domain.RunStatusFailure
	case "canceled":
		return domain.RunStatusAborted
	case "running", "pending", "created":
		return domain.RunStatusRunning
	default:
		return domain.RunStatusPending
	}
}

func mapGLResult(status string) domain.RunResult {
	switch status {
	case "success":
		return domain.RunResultSuccess
	case "failed":
		return domain.RunResultFailure
	case "canceled":
		return domain.RunResultAborted
	default:
		return ""
	}
}
