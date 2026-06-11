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

// compile-time assertions
var _ driven.CICore         = (*Adapter)(nil)
var _ driven.CITriggerable  = (*Adapter)(nil)
var _ driven.CIHistorical   = (*Adapter)(nil)
var _ driven.CIPipeliner    = (*Adapter)(nil)
var _ driven.CIArtifactStore = (*Adapter)(nil)
var _ driven.CIChainable    = (*Adapter)(nil)

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
func (a *Adapter) Type() string { return BackendName }
func (a *Adapter) Capabilities() driven.CapabilitySet {
	return driven.CapTrigger | driven.CapHistory | driven.CapStages | driven.CapArtifacts | driven.CapChain
}

// ── CICore ────────────────────────────────────────────────────────────────────

func (a *Adapter) GetRun(ctx context.Context, _ string, runID string) (*domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_run", slog.String("run_id", runID))

	var path string
	if runID == "latest" {
		path = fmt.Sprintf("/api/v4/projects/%s/pipelines?per_page=1", a.projectID)
		var resp []glPipeline
		if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
			adapterdriven.LogError(ctx, a.name, "get_run", err)
			return nil, err
		}
		if len(resp) == 0 {
			return nil, fmt.Errorf("%w: no pipelines found", ErrNotFound)
		}
		run := resp[0].toCIRun()
		adapterdriven.LogOpDone(ctx, a.name, "get_run",
			slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
		return &run, nil
	}

	path = fmt.Sprintf("/api/v4/projects/%s/pipelines/%s", a.projectID, runID)
	var resp glPipeline
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "get_run", err)
		return nil, err
	}
	run := resp.toCIRun()
	adapterdriven.LogOpDone(ctx, a.name, "get_run",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return &run, nil
}

func (a *Adapter) SearchRuns(ctx context.Context, _ string, f domain.BuildFilter) ([]domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "search_runs")

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	fetch := limit * 3
	if fetch < 50 {
		fetch = 50
	}

	params := url.Values{}
	params.Set("per_page", strconv.Itoa(fetch))
	if f.Result != "" {
		params.Set("status", mapResultToGLStatus(f.Result))
	}
	if f.Runner != "" {
		params.Set("username", f.Runner)
	}

	path := fmt.Sprintf("/api/v4/projects/%s/pipelines?%s", a.projectID, params.Encode())
	var resp []glPipeline
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "search_runs", err)
		return nil, err
	}

	var runs []domain.CIRun
	for _, p := range resp {
		if len(runs) >= limit {
			break
		}
		run := p.toCIRun()
		if !f.Since.IsZero() && run.StartedAt.Before(f.Since) {
			continue
		}
		runs = append(runs, run)
	}

	adapterdriven.LogOpDone(ctx, a.name, "search_runs",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(runs)))
	return runs, nil
}

func (a *Adapter) GetLog(ctx context.Context, _ string, runID string, _ domain.LogFilter) (string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_log", slog.String("run_id", runID))

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
		adapterdriven.LogError(ctx, a.name, "get_log", err)
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
	adapterdriven.LogOpDone(ctx, a.name, "get_log",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return string(data), nil
}

func (a *Adapter) CancelRun(_ context.Context, _, _ string) error { return nil }

// ── CITriggerable ─────────────────────────────────────────────────────────────

// Trigger creates a GitLab pipeline. GitLab returns the pipeline ID
// immediately, so NeedsResolve=false — ResolveReceipt is a no-op.
func (a *Adapter) Trigger(ctx context.Context, jobRef string, params map[string]string) (*domain.TriggerReceipt, error) {
	if a.token == "" {
		return nil, ErrAuthRequired
	}
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "trigger", slog.String(adapterdriven.LogKeyID, jobRef))

	ref := "main"
	if params != nil {
		if r, ok := params["ref"]; ok {
			ref = r
		}
	}

	path := fmt.Sprintf("/api/v4/projects/%s/pipeline?ref=%s", a.projectID, ref)
	var resp glPipeline
	if err := a.api(ctx, http.MethodPost, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "trigger", err)
		return nil, err
	}

	runID := strconv.FormatInt(resp.ID, 10)
	adapterdriven.LogOpDone(ctx, a.name, "trigger",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.String("run_id", runID))
	return &domain.TriggerReceipt{
		RunID:        runID,
		NeedsResolve: false,
		Backend:      a.name,
		JobRef:       jobRef,
	}, nil
}

// ResolveReceipt is a no-op for GitLab — pipeline ID is always available immediately.
func (a *Adapter) ResolveReceipt(_ context.Context, r *domain.TriggerReceipt) (*domain.TriggerReceipt, error) {
	return r, nil
}

func (a *Adapter) EstimateDuration(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

// ── CIHistorical ──────────────────────────────────────────────────────────────

func (a *Adapter) ListRuns(ctx context.Context, _ string, limit int) ([]domain.CIRun, error) {
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

func (a *Adapter) GetRunParams(_ context.Context, _, _ string) (map[string]string, error) {
	return nil, fmt.Errorf("%w: GitLab CI pipeline variables not yet supported", ErrNotFound)
}

// ── CIPipeliner ───────────────────────────────────────────────────────────────

func (a *Adapter) ListStages(ctx context.Context, _ string, runID string) ([]domain.CIJob, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_stages", slog.String("run_id", runID))

	path := fmt.Sprintf("/api/v4/projects/%s/pipelines/%s/jobs?per_page=100", a.projectID, runID)
	var resp []glJob
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "list_stages", err)
		return nil, err
	}

	jobs := make([]domain.CIJob, len(resp))
	for i := range resp {
		jobs[i] = resp[i].toCIJob()
	}
	adapterdriven.LogOpDone(ctx, a.name, "list_stages",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(jobs)))
	return jobs, nil
}

// ── CIArtifactStore ───────────────────────────────────────────────────────────

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

// ── CIChainable — bridges API ─────────────────────────────────────────────────

// GetDownstreamRuns uses the GitLab bridges API to find child pipelines
// triggered by pipeline upstreamRunID. The downstreamJob and upstreamJob
// params are informational; GitLab bridges are scoped to the parent pipeline.
func (a *Adapter) GetDownstreamRuns(ctx context.Context, _, _, upstreamRunID string) ([]domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_downstream_runs",
		slog.String("upstream_run_id", upstreamRunID))

	path := fmt.Sprintf("/api/v4/projects/%s/pipelines/%s/bridges?per_page=100",
		a.projectID, upstreamRunID)

	var bridges []struct {
		DownstreamPipeline *glPipeline `json:"downstream_pipeline"`
	}
	if err := a.api(ctx, http.MethodGet, path, nil, &bridges); err != nil {
		adapterdriven.LogError(ctx, a.name, "get_downstream_runs", err)
		return nil, err
	}

	var runs []domain.CIRun
	for _, b := range bridges {
		if b.DownstreamPipeline == nil {
			continue
		}
		run := b.DownstreamPipeline.toCIRun()
		run.UpstreamRunID = upstreamRunID
		runs = append(runs, run)
	}

	adapterdriven.LogOpDone(ctx, a.name, "get_downstream_runs",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(runs)))
	return runs, nil
}

// ── HTTP helper ───────────────────────────────────────────────────────────────

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

// ── Response types ────────────────────────────────────────────────────────────

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
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Stage     string  `json:"stage"`
	Status    string  `json:"status"`
	WebURL    string  `json:"web_url"`
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

func mapResultToGLStatus(result string) string {
	switch strings.ToUpper(result) {
	case "SUCCESS":
		return "success"
	case "FAILURE":
		return "failed"
	case "ABORTED":
		return "canceled"
	default:
		return ""
	}
}

func (a *Adapter) ListStageNodes(_ context.Context, _, _ string) ([]domain.CIStageNode, error) {
	return nil, fmt.Errorf("stage step expansion not supported by %s backend", a.Name())
}

func (a *Adapter) ListWfArtifacts(ctx context.Context, jobRef, runID string) ([]domain.CIArtifact, error) {
	return a.ListArtifacts(ctx, jobRef, runID)
}

func (a *Adapter) ListStageNodesWithLogs(ctx context.Context, jobRef, runID string) ([]domain.CIStageNode, error) {
	return a.ListStageNodes(ctx, jobRef, runID)
}
