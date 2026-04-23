package domain

import "time"

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
