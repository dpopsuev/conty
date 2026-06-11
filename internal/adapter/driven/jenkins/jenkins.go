package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	adapterdriven "github.com/dpopsuev/conty/internal/adapter/driven"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
	"github.com/bndr/gojenkins"
)

const BackendName = "jenkins"

// descriptionString safely converts the gojenkins Raw.Description interface{} value to string.
func descriptionString(v interface{}) string {
	s, _ := v.(string)
	return s
}

// hrefRe matches href attributes in HTML, capturing the URL value.
var hrefRe = regexp.MustCompile(`href=['"]([^'"]+)['"]`)

// parseChildrenFromDescription extracts child job/run pairs from a Jenkins build
// description field. Jenkins encodes triggered downstream builds as HTML anchor
// elements whose href follows the /job/A/job/B/NNN/ pattern. The description
// field is not machine-readable in the standard API; this is the only way to
// discover which child build numbers were produced by a parent pipeline build.
func parseChildrenFromDescription(desc string) []domain.CIRunRef {
	if desc == "" {
		return nil
	}
	var results []domain.CIRunRef
	seen := map[string]bool{}
	for _, m := range hrefRe.FindAllStringSubmatch(desc, -1) {
		href := m[1]
		// Strip scheme+host from absolute URLs, keep path only.
		if idx := strings.Index(href, "/job/"); idx > 0 {
			href = href[idx:]
		}
		// Parse /job/A/job/B/.../NNN[/] into jobRef="A/B/..." and runID="NNN".
		segments := strings.Split(strings.Trim(href, "/"), "/")
		var jobParts []string
		var runID string
		for i, s := range segments {
			if s == "job" {
				continue
			}
			if i > 0 && segments[i-1] == "job" {
				jobParts = append(jobParts, s)
			} else if _, err := strconv.Atoi(s); err == nil {
				runID = s
			}
		}
		if len(jobParts) == 0 || runID == "" {
			continue
		}
		key := strings.Join(jobParts, "/") + "/" + runID
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, domain.CIRunRef{
			JobRef: strings.Join(jobParts, "/"),
			RunID:  runID,
		})
	}
	return results
}

var _ driven.CICore         = (*Adapter)(nil)
var _ driven.CITriggerable  = (*Adapter)(nil)
var _ driven.CIHistorical   = (*Adapter)(nil)
var _ driven.CIPipeliner    = (*Adapter)(nil)
var _ driven.CIArtifactStore = (*Adapter)(nil)
var _ driven.CIChainable    = (*Adapter)(nil)

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

func New(name, baseURL, user, token string) (*Adapter, error) {
	j := gojenkins.CreateJenkins(nil, baseURL, user, token)
	return &Adapter{name: name, jenkins: j, baseURL: baseURL, user: user, token: token}, nil
}

func (a *Adapter) Name() string { return a.name }
func (a *Adapter) Type() string { return BackendName }

func (a *Adapter) Capabilities() driven.CapabilitySet {
	return driven.CapTrigger | driven.CapHistory | driven.CapStages | driven.CapArtifacts | driven.CapChain
}

func (a *Adapter) Trigger(ctx context.Context, jobName string, params map[string]string) (*domain.TriggerReceipt, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "trigger", slog.String(adapterdriven.LogKeyID, jobName))

	queueID, err := a.jenkins.BuildJob(ctx, jobName, params)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "trigger", err)
		return nil, err
	}

	adapterdriven.LogOpDone(ctx, a.name, "trigger",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int64("queue_id", queueID))
	return &domain.TriggerReceipt{
		OpaqueRef:    strconv.FormatInt(queueID, 10),
		NeedsResolve: true,
		Backend:      a.name,
		JobRef:       jobName,
	}, nil
}

func (a *Adapter) ResolveReceipt(ctx context.Context, r *domain.TriggerReceipt) (*domain.TriggerReceipt, error) {
	resolved := *r
	buildNum, err := a.PollQueue(ctx, r.OpaqueRef)
	if err != nil {
		return &resolved, err
	}
	if buildNum != "" {
		resolved.RunID = buildNum
		resolved.NeedsResolve = false
	}
	return &resolved, nil
}

func (a *Adapter) EstimateDuration(ctx context.Context, jobName string) (int64, error) {
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

func (a *Adapter) GetRun(ctx context.Context, jobName string, runID string) (*domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_run",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_run", err)
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
		adapterdriven.LogError(ctx, a.name, "get_run", err)
		return nil, fmt.Errorf("%w: %s #%s", ErrBuildNotFound, jobName, runID)
	}

	run := a.mapBuild(ctx, b)
	adapterdriven.LogOpDone(ctx, a.name, "get_run",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)))
	return run, nil
}

func (a *Adapter) ListStages(ctx context.Context, jobName string, runID string) ([]domain.CIJob, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_stages",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_stages", err)
		return nil, err
	}

	raw, err := j.GetPipelineRun(ctx, runID)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_stages", err)
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

	adapterdriven.LogOpDone(ctx, a.name, "list_stages",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(jobs)))
	return jobs, nil
}

func (a *Adapter) ListStageNodes(ctx context.Context, jobName string, runID string) ([]domain.CIStageNode, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_stage_nodes",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_stage_nodes", err)
		return nil, err
	}

	raw, err := j.GetPipelineRun(ctx, runID)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_stage_nodes", err)
		return nil, err
	}

	nodes := make([]domain.CIStageNode, 0, len(raw.Stages))
	for i := range raw.Stages {
		s := &raw.Stages[i]
		node := domain.CIStageNode{
			ID:       s.ID,
			Name:     s.Name,
			Status:   mapPipelineStatus(s.Status),
			Duration: int64(s.Duration),
		}
		// Fetch steps for non-skipped stages.
		if s.Status != "NOT_EXECUTED" && s.Status != "SKIPPED" {
			detail, nerr := raw.GetNode(ctx, s.ID)
			if nerr == nil {
				node.Steps = make([]domain.CIStep, 0, len(detail.StageFlowNodes))
				for j := range detail.StageFlowNodes {
					st := &detail.StageFlowNodes[j]
					node.Steps = append(node.Steps, domain.CIStep{
						ID:          st.ID,
						Name:        st.Name,
						Status:      mapPipelineStatus(st.Status),
						Duration:    int64(st.Duration),
						Description: st.Name,
					})
				}
			}
		}
		nodes = append(nodes, node)
	}

	adapterdriven.LogOpDone(ctx, a.name, "list_stage_nodes",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(nodes)))
	return nodes, nil
}

func (a *Adapter) ListWfArtifacts(ctx context.Context, jobName string, runID string) ([]domain.CIArtifact, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "list_wf_artifacts",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_wf_artifacts", err)
		return nil, err
	}

	run, err := j.GetPipelineRun(ctx, runID)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_wf_artifacts", err)
		return nil, err
	}

	raw, err := run.GetArtifacts(ctx)
	if err != nil {
		// wfapi artifacts endpoint may not always be present; fall through to empty.
		adapterdriven.LogError(ctx, a.name, "list_wf_artifacts", err)
		return nil, err
	}

	artifacts := make([]domain.CIArtifact, 0, len(raw))
	for _, art := range raw {
		artifacts = append(artifacts, domain.CIArtifact{
			Name: art.Name,
			Path: wfArtifactRelPath(art.Path, art.Name),
			Size: int64(art.Size),
		})
	}

	adapterdriven.LogOpDone(ctx, a.name, "list_wf_artifacts",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(artifacts)))
	return artifacts, nil
}

func (a *Adapter) GetLog(ctx context.Context, jobName string, runID string, _ domain.LogFilter) (string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_log",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	j, err := a.getJob(ctx, jobName)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_log", err)
		return "", err
	}

	num, parseErr := strconv.ParseInt(runID, 10, 64)
	if parseErr != nil {
		return "", fmt.Errorf("invalid run ID %q: %w", runID, parseErr)
	}

	b, err := j.GetBuild(ctx, num)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_log", err)
		return "", fmt.Errorf("%w: %s #%s", ErrBuildNotFound, jobName, runID)
	}

	output := b.GetConsoleOutput(ctx)
	adapterdriven.LogOpDone(ctx, a.name, "get_log",
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
		// gojenkins sets Path = base + "/artifact/" + relativePath (full URL).
		// Strip to get the relative path for tree building.
		artifacts = append(artifacts, domain.CIArtifact{
			Name: raw[i].FileName,
			Path: wfArtifactRelPath(raw[i].Path, raw[i].FileName),
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

// pollQueue resolves a Jenkins build queue item to a build number.
// Used internally by ResolveReceipt. Returns empty string when the
// item is not yet assigned to an executor.
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

func (a *Adapter) GetRunParams(ctx context.Context, jobName string, runID string) (map[string]string, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_run_params",
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
		adapterdriven.LogError(ctx, a.name, "get_run_params", err)
		return nil, fmt.Errorf("%w: %s #%s", ErrBuildNotFound, jobName, runID)
	}

	params := make(map[string]string)
	for _, p := range b.GetParameters() {
		params[p.Name] = fmt.Sprintf("%v", p.Value)
	}

	adapterdriven.LogOpDone(ctx, a.name, "get_run_params",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(params)))
	return params, nil
}

// buildJobPath converts a folder-scoped Jenkins job name (e.g. "CI/my-job") into
// the URL segment expected by the raw Jenkins API (e.g. "/job/CI/job/my-job").
func buildJobPath(jobName string) string {
	parts := strings.Split(jobName, "/")
	var path string
	for _, p := range parts {
		path += "/job/" + p
	}
	return path
}

// buildAction is a single entry from the Jenkins build actions array.
// It covers parameter sets and cause chains including upstream build references.
type buildAction struct {
	Parameters []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	} `json:"parameters"`
	Causes []struct {
		UserID          string `json:"userId"`
		UserName        string `json:"userName"`
		UpstreamBuild   int64  `json:"upstreamBuild"`
		UpstreamProject string `json:"upstreamProject"`
	} `json:"causes"`
}

// mapCauses extracts the upstream job name and build number from a buildAction
// slice. Returns empty strings when no upstream cause is present.
func mapCauses(actions []buildAction) (upstreamJob, upstreamRunID string) {
	for _, a := range actions {
		for _, c := range a.Causes {
			if c.UpstreamProject != "" && c.UpstreamBuild != 0 {
				return c.UpstreamProject, strconv.FormatInt(c.UpstreamBuild, 10)
			}
		}
	}
	return
}

// extractUpstreamFromBuild reads upstream cause fields from a gojenkins Build's
// raw action array without making an additional HTTP call. Jenkins encodes
// upstreamBuild as a JSON number that Go's JSON decoder maps to float64 when
// decoded into the generalObj map[string]interface{} fields.
func extractUpstreamFromBuild(b *gojenkins.Build) (upstreamJob, upstreamRunID string) {
	for _, a := range b.Raw.Actions {
		for _, c := range a.Causes {
			proj, _ := c["upstreamProject"].(string)
			var num int64
			switch v := c["upstreamBuild"].(type) {
			case float64:
				num = int64(v)
			case int64:
				num = v
			}
			if proj != "" && num != 0 {
				return proj, strconv.FormatInt(num, 10)
			}
		}
	}
	return
}

// listRunsResponse is the minimal Jenkins API shape used by ListBuilds.
type listRunsResponse struct {
	Builds []struct {
		Number          int64  `json:"number"`
		Result          string `json:"result"`
		FullDisplayName string `json:"fullDisplayName"`
		Timestamp       int64  `json:"timestamp"`
		Duration        int64  `json:"duration"`
		URL             string `json:"url"`
		Building        bool   `json:"building"`
	} `json:"builds"`
}

func (a *Adapter) ListRuns(ctx context.Context, jobName string, limit int) ([]domain.CIRun, error) {
	start := time.Now()
	if limit <= 0 {
		limit = 10
	}
	adapterdriven.LogOp(ctx, a.name, "list_runs",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.Int("limit", limit))

	// Build the job URL, handling folder paths (e.g. "CI/my-job" → /job/CI/job/my-job).
	jobPath := buildJobPath(jobName)
	treeParam := fmt.Sprintf(
		"builds[number,result,fullDisplayName,timestamp,duration,url,building]{0,%d}",
		limit,
	)
	apiURL := strings.TrimRight(a.baseURL, "/") + jobPath +
		"/api/json?tree=" + treeParam

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(a.user, a.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "list_runs", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s (HTTP %d)", ErrJobNotFound, jobName, resp.StatusCode)
	}

	var payload listRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("list_builds decode: %w", err)
	}

	runs := make([]domain.CIRun, 0, len(payload.Builds))
	for _, b := range payload.Builds {
		status := domain.RunStatusSuccess
		switch {
		case b.Building:
			status = domain.RunStatusRunning
		case b.Result == "FAILURE":
			status = domain.RunStatusFailure
		case b.Result == "ABORTED":
			status = domain.RunStatusAborted
		case b.Result == "":
			status = domain.RunStatusPending
		}
		runs = append(runs, domain.CIRun{
			ID:        strconv.FormatInt(b.Number, 10),
			Name:      b.FullDisplayName,
			Status:    status,
			Result:    domain.RunResult(b.Result),
			URL:       b.URL,
			StartedAt: time.UnixMilli(b.Timestamp),
			Duration:  b.Duration,
		})
	}

	adapterdriven.LogOpDone(ctx, a.name, "list_runs",
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

	upstreamJob, upstreamRunID := extractUpstreamFromBuild(b)
	return &domain.CIRun{
		ID:            strconv.FormatInt(b.GetBuildNumber(), 10),
		Name:          b.Raw.FullDisplayName,
		Status:        status,
		Result:        domain.RunResult(b.GetResult()),
		URL:           b.GetUrl(),
		StartedAt:     b.GetTimestamp(),
		Duration:      int64(b.GetDuration()),
		UpstreamJob:   upstreamJob,
		UpstreamRunID: upstreamRunID,
		Children:      parseChildrenFromDescription(descriptionString(b.Raw.Description)),
	}
}

func (a *Adapter) CancelRun(ctx context.Context, jobName string, runID string) error {
	adapterdriven.LogOp(ctx, a.name, "cancel_run",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.String("run_id", runID))

	jobPath := buildJobPath(jobName)
	stopURL := strings.TrimRight(a.baseURL, "/") + jobPath + "/" + runID + "/stop"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stopURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(a.user, a.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "cancel_run", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("cancel_run %s #%s: HTTP %d", jobName, runID, resp.StatusCode)
	}
	return nil
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

type buildWithParams struct {
	Number          int64  `json:"number"`
	Result          string `json:"result"`
	FullDisplayName string `json:"fullDisplayName"`
	Timestamp       int64  `json:"timestamp"`
	Duration        int64  `json:"duration"`
	EstimatedDuration int64 `json:"estimatedDuration"`
	Description     string `json:"description"`
	URL             string `json:"url"`
	Building        bool   `json:"building"`
	Culprits        []struct {
		ID       string `json:"id"`
		FullName string `json:"fullName"`
	} `json:"culprits"`
	Actions         []buildAction `json:"actions"`
}

// searchPageSize is the number of builds fetched per Jenkins API page in SearchRuns.
const searchPageSize = 50

// searchMaxPages caps the total pages fetched to prevent runaway loops on
// unbounded searches (no since, rare params match).
const searchMaxPages = 200

func (a *Adapter) SearchRuns(ctx context.Context, jobName string, f domain.BuildFilter) ([]domain.CIRun, error) {
	start := time.Now()
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}

	adapterdriven.LogOp(ctx, a.name, "search_runs",
		slog.String(adapterdriven.LogKeyID, jobName),
		slog.Int("limit", limit))

	var runs []domain.CIRun
	for page := range searchMaxPages {
		offset := page * searchPageSize
		builds, err := a.fetchSearchPage(ctx, jobName, offset, searchPageSize)
		if err != nil {
			adapterdriven.LogError(ctx, a.name, "search_runs", err)
			return nil, err
		}

		done := false
		for _, b := range builds {
			if len(runs) >= limit {
				done = true
				break
			}
			// Jenkins returns builds newest-first. The first build before f.Since
			// means all remaining builds are also before it.
			if !f.Since.IsZero() && time.UnixMilli(b.Timestamp).Before(f.Since) {
				done = true
				break
			}
			run, ok := a.filterAndMapBuild(ctx, jobName, b, f)
			if !ok {
				continue
			}
			runs = append(runs, run)
		}

		if done || len(builds) < searchPageSize {
			break
		}
	}

	adapterdriven.LogOpDone(ctx, a.name, "search_runs",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(runs)))
	return runs, nil
}

// fetchSearchPage fetches one page of builds from the Jenkins API.
// offset and count map to the Jenkins tree range {offset, offset+count}.
func (a *Adapter) fetchSearchPage(ctx context.Context, jobName string, offset, count int) ([]buildWithParams, error) {
	jobPath := buildJobPath(jobName)
	treeParam := fmt.Sprintf(
		"builds[number,result,fullDisplayName,timestamp,duration,estimatedDuration,url,building,description,"+
			"culprits[id,fullName],"+
			"actions[parameters[name,value],causes[userId,userName,shortDescription,upstreamBuild,upstreamProject],text]]{%d,%d}",
		offset, offset+count,
	)
	apiURL := strings.TrimRight(a.baseURL, "/") + jobPath + "/api/json?tree=" + treeParam

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(a.user, a.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s (HTTP %d)", ErrJobNotFound, jobName, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("search_builds read: %w", err)
	}

	var payload struct {
		Builds []buildWithParams `json:"builds"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("search_builds decode: %w", err)
	}
	return payload.Builds, nil
}

// filterAndMapBuild applies result, runner, and params filters to a build and
// maps it to a domain.CIRun. Returns (run, false) if the build is filtered out.
func (a *Adapter) filterAndMapBuild(ctx context.Context, jobName string, b buildWithParams, f domain.BuildFilter) (domain.CIRun, bool) {
	// Result filter
	if f.Result != "" && !strings.EqualFold(b.Result, f.Result) {
		return domain.CIRun{}, false
	}

	// Runner filter — match userId or userName from causes
	if f.Runner != "" {
		matched := false
		for _, action := range b.Actions {
			for _, cause := range action.Causes {
				if strings.EqualFold(cause.UserID, f.Runner) || strings.EqualFold(cause.UserName, f.Runner) {
					matched = true
				}
			}
		}
		if !matched {
			return domain.CIRun{}, false
		}
	}

	// Params filter — all specified key=value pairs must match
	if len(f.Params) > 0 {
		got := map[string]string{}
		for _, action := range b.Actions {
			for _, p := range action.Parameters {
				got[p.Name] = fmt.Sprintf("%v", p.Value)
			}
		}
		for k, v := range f.Params {
			if got[k] != v {
				return domain.CIRun{}, false
			}
		}
	}

	status := domain.RunStatusSuccess
	switch {
	case b.Building:
		status = domain.RunStatusRunning
	case b.Result == "FAILURE":
		status = domain.RunStatusFailure
	case b.Result == "ABORTED":
		status = domain.RunStatusAborted
	case b.Result == "":
		status = domain.RunStatusPending
	}

	for _, action := range b.Actions {
		for _, c := range action.Causes {
			if c.UpstreamProject != "" && c.UpstreamBuild == 0 {
				slog.LogAttrs(ctx, slog.LevelWarn, "upstream cause missing build number",
					slog.String("upstream_project", c.UpstreamProject),
					slog.Int64("build_number", b.Number))
			}
		}
	}
	upstreamJob, upstreamRunID := mapCauses(b.Actions)
	if upstreamJob != "" {
		slog.LogAttrs(ctx, slog.LevelDebug, "upstream cause mapped",
			slog.String("job", jobName),
			slog.Int64("build", b.Number),
			slog.String("upstream_job", upstreamJob),
			slog.String("upstream_run_id", upstreamRunID))
	}
	return domain.CIRun{
		ID:            strconv.FormatInt(b.Number, 10),
		Name:          b.FullDisplayName,
		Status:        status,
		Result:        domain.RunResult(b.Result),
		URL:           b.URL,
		StartedAt:     time.UnixMilli(b.Timestamp),
		Duration:      b.Duration,
		UpstreamJob:   upstreamJob,
		UpstreamRunID: upstreamRunID,
		Children:      parseChildrenFromDescription(b.Description),
	}, true
}

// GetDownstreamRuns finds builds in downstreamJob that were triggered by
// upstreamJob#upstreamRunID. Jenkins has no native reverse index, so this
// fetches recent builds and filters client-side on the upstream cause chain.
func (a *Adapter) GetDownstreamRuns(ctx context.Context, downstreamJob, upstreamJob, upstreamRunID string) ([]domain.CIRun, error) {
	start := time.Now()
	adapterdriven.LogOp(ctx, a.name, "get_downstream_runs",
		slog.String("downstream_job", downstreamJob),
		slog.String("upstream_job", upstreamJob),
		slog.String("upstream_run_id", upstreamRunID))

	parentNum, err := strconv.ParseInt(upstreamRunID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream_run_id %q: %w", upstreamRunID, err)
	}

	jobPath := buildJobPath(downstreamJob)
	const treeParam = "builds[number,result,fullDisplayName,timestamp,duration,url,building," +
		"actions[causes[upstreamBuild,upstreamProject]]]{0,50}"
	apiURL := strings.TrimRight(a.baseURL, "/") + jobPath + "/api/json?tree=" + treeParam

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(a.user, a.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		adapterdriven.LogError(ctx, a.name, "get_downstream_runs", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Orange: non-200 from Jenkins.
		apiErr := fmt.Errorf("%w: %s (HTTP %d)", ErrJobNotFound, downstreamJob, resp.StatusCode)
		adapterdriven.LogError(ctx, a.name, "get_downstream_runs", apiErr)
		return nil, apiErr
	}

	var payload struct {
		Builds []struct {
			Number          int64         `json:"number"`
			Result          string        `json:"result"`
			FullDisplayName string        `json:"fullDisplayName"`
			Timestamp       int64         `json:"timestamp"`
			Duration        int64         `json:"duration"`
			URL             string        `json:"url"`
			Building        bool          `json:"building"`
			Actions         []buildAction `json:"actions"`
		} `json:"builds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		adapterdriven.LogError(ctx, a.name, "get_downstream_runs", err)
		return nil, fmt.Errorf("get_downstream_runs decode: %w", err)
	}

	var runs []domain.CIRun
	for _, b := range payload.Builds {
		// Filter client-side: keep builds whose cause chain references the parent.
		matched := false
		for _, action := range b.Actions {
			for _, c := range action.Causes {
				if strings.EqualFold(c.UpstreamProject, upstreamJob) && c.UpstreamBuild == parentNum {
					matched = true
				}
			}
		}
		if !matched {
			continue
		}

		status := domain.RunStatusSuccess
		switch {
		case b.Building:
			status = domain.RunStatusRunning
		case b.Result == "FAILURE":
			status = domain.RunStatusFailure
		case b.Result == "ABORTED":
			status = domain.RunStatusAborted
		case b.Result == "":
			status = domain.RunStatusPending
		}

		uj, uri := mapCauses(b.Actions)
		runs = append(runs, domain.CIRun{
			ID:            strconv.FormatInt(b.Number, 10),
			Name:          b.FullDisplayName,
			Status:        status,
			Result:        domain.RunResult(b.Result),
			URL:           b.URL,
			StartedAt:     time.UnixMilli(b.Timestamp),
			Duration:      b.Duration,
			UpstreamJob:   uj,
			UpstreamRunID: uri,
		})
	}

	// Yellow: success signal with result count.
	adapterdriven.LogOpDone(ctx, a.name, "get_downstream_runs",
		slog.Duration(adapterdriven.LogKeyElapsed, time.Since(start)),
		slog.Int(adapterdriven.LogKeyCount, len(runs)))
	return runs, nil
}

// wfArtifactRelPath extracts the relative artifact path from gojenkins'
// PipelineArtifact.Path, which may be an absolute URL path like
// "/job/foo/42/artifact/reports/file.xml". Falls back to name.
func wfArtifactRelPath(path, name string) string {
	if idx := strings.Index(path, "/artifact/"); idx >= 0 {
		return path[idx+len("/artifact/"):]
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		return path
	}
	return name
}
