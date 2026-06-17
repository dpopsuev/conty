package domain

import "time"

// BuildFilter selects past builds to return from SearchBuilds.
// All non-zero fields are ANDed together.
type BuildFilter struct {
	Result        string            // e.g. "SUCCESS", "FAILURE", "ABORTED"
	Params        map[string]string // all specified params must match (exact)
	ParamsContain map[string]string // all specified params must contain substring (case-insensitive)
	Runner        string            // user who triggered the build (userId from causes)
	Since         time.Time         // only builds started at or after this time
	Limit         int               // max results to return (default 20)
}

const (
	// LogDefaultTail is the default number of tail lines returned from a build log.
	// Jenkins logs grow large; failures always appear at the end.
	LogDefaultTail = 200
	// LogDefaultMaxBytes caps the returned log regardless of line count.
	LogDefaultMaxBytes = 50 * 1024
)

// LogFilter controls how much of a build log is returned.
type LogFilter struct {
	// Tail is the max lines to return from the end; 0 means use LogDefaultTail.
	// Set to -1 to return all lines (no truncation).
	Tail int
	// Grep filters to lines containing this substring (case-insensitive) before tail is applied.
	Grep string
}

// LogResult is the return value of CILog.
type LogResult struct {
	Lines      []string `json:"lines"`
	TotalLines int      `json:"total_lines"`
	Skipped    int      `json:"skipped,omitempty"`
	Filtered   bool     `json:"filtered,omitempty"` // true if grep was applied
	Truncated  bool     `json:"truncated,omitempty"`
}

// CIRunRef is a lightweight pointer to a specific build of a job.
// Populated from the Jenkins build description when the parent build
// triggered downstream jobs whose run IDs can be parsed from HTML links.
type CIRunRef struct {
	JobRef      string `json:"job_ref"`
	RunID       string `json:"run_id"`
	DisplayName string `json:"display_name,omitempty"`
}

type CIRun struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Status        RunStatus  `json:"status"`
	Result        RunResult  `json:"result,omitempty"`
	URL           string     `json:"url,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	Duration      int64      `json:"duration,omitempty"`
	UpstreamJob   string     `json:"upstream_job,omitempty"`
	UpstreamRunID string     `json:"upstream_run_id,omitempty"`
	Children       []CIRunRef `json:"children,omitempty"`
	FailureExcerpt string     `json:"failure_excerpt,omitempty"`
}

// CIRunNode is a fully-expanded build with its children recursively resolved.
// Returned by the chain action in place of the lightweight CIRunRef slice.
type CIRunNode struct {
	JobRef      string        `json:"job_ref"`
	RunID       string        `json:"run_id"`
	Name        string        `json:"name"`
	DisplayName string        `json:"display_name,omitempty"`
	Status      RunStatus     `json:"status"`
	Result      RunResult     `json:"result,omitempty"`
	URL         string        `json:"url,omitempty"`
	Duration    int64         `json:"duration,omitempty"`
	Artifacts   []CIArtifact  `json:"artifacts,omitempty"`
	Children    []CIRunNode   `json:"children,omitempty"`
}

// CIStep is a single step within a pipeline stage.
type CIStep struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      RunStatus `json:"status"`
	Duration    int64     `json:"duration_ms,omitempty"`
	DurationStr string    `json:"duration,omitempty"`
	Description string    `json:"description,omitempty"`
	FailedLog   string    `json:"failed_log,omitempty"`
}

// CIStageNode is a pipeline stage with its steps expanded.
type CIStageNode struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      RunStatus `json:"status"`
	Duration    int64     `json:"duration_ms,omitempty"`
	DurationStr string    `json:"duration,omitempty"`
	Steps       []CIStep  `json:"steps,omitempty"`
}

// CIArtifactDir is a node in an artifact directory tree.
// Files are artifacts directly in this directory; Children are subdirs.
type CIArtifactDir struct {
	Path     string          `json:"path"`
	Files    []CIArtifact    `json:"files,omitempty"`
	Children []CIArtifactDir `json:"children,omitempty"`
}

type CIJob struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    RunStatus `json:"status"`
	Result    RunResult `json:"result,omitempty"`
	ParentRun string    `json:"parent_run,omitempty"`
	URL       string    `json:"url,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Duration  int64     `json:"duration,omitempty"`
}

type CIArtifact struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
}
