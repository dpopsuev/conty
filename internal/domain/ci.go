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
