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
	"net/url"
	"strconv"
	"strings"
	"time"

	adapterdriven "github.com/dpopsuev/conty/internal/adapter/driven"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

const (
	apiURL      = "https://api.github.com"
	BackendName = "github"
)

// compile-time assertions — GitHub does NOT implement CIChainable.
var _ driven.CICore         = (*Adapter)(nil)
var _ driven.CITriggerable  = (*Adapter)(nil)
var _ driven.CIHistorical   = (*Adapter)(nil)
var _ driven.CIPipeliner    = (*Adapter)(nil)
var _ driven.CIArtifactStore = (*Adapter)(nil)

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
func (a *Adapter) Type() string { return BackendName }
func (a *Adapter) Capabilities() driven.CapabilitySet {
	return driven.CapTrigger | driven.CapHistory | driven.CapStages | driven.CapArtifacts
}

// ── CICore ────────────────────────────────────────────────────────────────────

func (a *Adapter) GetRun(ctx context.Context, jobName string, runID string) (*domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_run",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	var path string
	if runID == "latest" {
		path = fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=1", a.owner, a.repo)
		var resp struct {
			WorkflowRuns []ghWorkflowRun `json:"workflow_runs"`
		}
		if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
			adapterdriven.LogError(ctx, a.name, "get_run", err)
			return nil, err
		}
		if len(resp.WorkflowRuns) == 0 {
			return nil, fmt.Errorf("%w: no runs found", ErrNotFound)
		}
		run := resp.WorkflowRuns[0].toCIRun()
		adapterdriven.LogOpDone(ctx, a.name, "get_run",
			slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
		return &run, nil
	}

	path = fmt.Sprintf("/repos/%s/%s/actions/runs/%s", a.owner, a.repo, runID)
	var resp ghWorkflowRun
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
		params.Set("status", mapResultToGHStatus(f.Result))
	}
	if f.Runner != "" {
		params.Set("actor", f.Runner)
	}

	path := fmt.Sprintf("/repos/%s/%s/actions/runs?%s", a.owner, a.repo, params.Encode())
	var resp struct {
		WorkflowRuns []ghWorkflowRun `json:"workflow_runs"`
	}
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "search_runs", err)
		return nil, err
	}

	var runs []domain.CIRun
	for _, wr := range resp.WorkflowRuns {
		if len(runs) >= limit {
			break
		}
		run := wr.toCIRun()
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

	logURL := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%s/logs", a.baseURL, a.owner, a.repo, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL, http.NoBody)
	if err != nil {
		return "", err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "token "+a.token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := a.client.Do(req)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_log", err)
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
	adapterdriven.LogOpDone(ctx, a.name, "get_log",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return string(data), nil
}

func (a *Adapter) CancelRun(ctx context.Context, _ string, runID string) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%s/cancel", a.owner, a.repo, runID)
	return a.api(ctx, http.MethodPost, path, nil, nil)
}

// ── CITriggerable ─────────────────────────────────────────────────────────────

func (a *Adapter) Trigger(ctx context.Context, jobRef string, params map[string]string) (*domain.TriggerReceipt, error) {
	if a.token == "" {
		return nil, ErrAuthRequired
	}
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "trigger", slog.String(adapterdriven.LogKeyID, jobRef))

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

	dispatchedAt := time.Now().UTC()
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches", a.owner, a.repo, jobRef)
	if err := a.api(ctx, http.MethodPost, path, body, nil); err != nil {
		adapterdriven.LogError(ctx, a.name, "trigger", err)
		return nil, err
	}

	adapterdriven.LogOpDone(ctx, a.name, "trigger",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return &domain.TriggerReceipt{
		OpaqueRef:    dispatchedAt.Format(time.RFC3339),
		NeedsResolve: true,
		Backend:      a.name,
		JobRef:       jobRef,
	}, nil
}

// ResolveReceipt polls for a workflow run created after the dispatch timestamp
// encoded in r.OpaqueRef. Returns the same receipt (NeedsResolve=true) when
// no matching run is found yet.
func (a *Adapter) ResolveReceipt(ctx context.Context, r *domain.TriggerReceipt) (*domain.TriggerReceipt, error) {
	since, err := time.Parse(time.RFC3339, r.OpaqueRef)
	if err != nil {
		// Orange: malformed OpaqueRef — log and treat as unresolved
		slog.LogAttrs(ctx, slog.LevelWarn, "github resolve_receipt: malformed opaque_ref",
			slog.String("opaque_ref", r.OpaqueRef),
			slog.String("error", err.Error()))
		return r, nil
	}

	path := fmt.Sprintf("/repos/%s/%s/actions/runs?event=workflow_dispatch&per_page=10", a.owner, a.repo)
	var resp struct {
		WorkflowRuns []ghWorkflowRun `json:"workflow_runs"`
	}
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return r, err
	}

	for _, run := range resp.WorkflowRuns {
		created, _ := time.Parse(time.RFC3339, run.CreatedAt)
		if created.After(since.Add(-5 * time.Second)) {
			resolved := *r
			resolved.RunID = strconv.FormatInt(run.ID, 10)
			resolved.NeedsResolve = false
			slog.LogAttrs(ctx, slog.LevelDebug, "github receipt resolved",
				slog.String("run_id", resolved.RunID))
			return &resolved, nil
		}
	}
	return r, nil // not yet available
}

func (a *Adapter) EstimateDuration(_ context.Context, _ string) (int64, error) {
	return 0, nil // GitHub Actions does not expose estimated duration
}

// ── CIHistorical ──────────────────────────────────────────────────────────────

func (a *Adapter) ListRuns(ctx context.Context, _ string, limit int) ([]domain.CIRun, error) {
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

func (a *Adapter) GetRunParams(_ context.Context, _, _ string) (map[string]string, error) {
	return nil, fmt.Errorf("%w: GitHub Actions workflow inputs not yet supported", ErrNotFound)
}

// ── CIPipeliner ───────────────────────────────────────────────────────────────

func (a *Adapter) ListStages(ctx context.Context, _ string, runID string) ([]domain.CIJob, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_stages", slog.String("run_id", runID))

	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%s/jobs", a.owner, a.repo, runID)
	var resp struct {
		Jobs []ghWorkflowJob `json:"jobs"`
	}
	if err := a.api(ctx, http.MethodGet, path, nil, &resp); err != nil {
		adapterdriven.LogError(ctx, a.name, "list_stages", err)
		return nil, err
	}

	jobs := make([]domain.CIJob, len(resp.Jobs))
	for i := range resp.Jobs {
		jobs[i] = resp.Jobs[i].toCIJob()
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

	artifactURL := fmt.Sprintf("%s/repos/%s/%s/actions/artifacts/%s/zip", a.baseURL, a.owner, a.repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, http.NoBody)
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

// ── HTTP helper ───────────────────────────────────────────────────────────────

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

// ── Response types ────────────────────────────────────────────────────────────

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

func mapResultToGHStatus(result string) string {
	switch strings.ToUpper(result) {
	case "SUCCESS":
		return "success"
	case "FAILURE":
		return "failure"
	case "ABORTED":
		return "cancelled"
	default:
		return ""
	}
}
