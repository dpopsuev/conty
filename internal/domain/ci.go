package domain

import "time"

// BuildFilter selects past builds to return from SearchBuilds.
// All non-zero fields are ANDed together.
type BuildFilter struct {
	Result string            // e.g. "SUCCESS", "FAILURE", "ABORTED"
	Params map[string]string // all specified params must match
	Runner string            // user who triggered the build (userId from causes)
	Since  time.Time         // only builds started at or after this time
	Limit  int               // max results to return (default 20)
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

type CIRun struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    RunStatus `json:"status"`
	Result    RunResult `json:"result,omitempty"`
	URL       string    `json:"url,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Duration  int64     `json:"duration,omitempty"`
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
