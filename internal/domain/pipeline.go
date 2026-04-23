package domain

import "time"

type Pipeline struct {
	Name    string         `json:"name"`
	Steps   []PipelineStep `json:"steps"`
	Backend string         `json:"backend"`
}

type PipelineStep struct {
	JobName string            `json:"job_name"`
	Params  map[string]string `json:"params,omitempty"`
}

type PipelineRun struct {
	Pipeline  string       `json:"pipeline"`
	Status    RunStatus    `json:"status"`
	Steps     []StepResult `json:"steps"`
	StartedAt time.Time    `json:"started_at"`
	Duration  int64        `json:"duration,omitempty"`
}

type StepResult struct {
	JobName   string    `json:"job_name"`
	RunID     string    `json:"run_id,omitempty"`
	Status    RunStatus `json:"status"`
	Result    RunResult `json:"result,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Duration  int64     `json:"duration,omitempty"`
	URL       string    `json:"url,omitempty"`
}
