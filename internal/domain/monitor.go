package domain

import "time"

type CICheck struct {
	JobRef    string    `json:"job_ref"`
	Backend   string    `json:"backend"`
	RunID     string    `json:"run_id"`
	Status    RunStatus `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
}

type CIVerdict struct {
	Check       CICheck         `json:"check"`
	TestSummary *TestSummary    `json:"test_summary,omitempty"`
	Failure     *FailureContext `json:"failure,omitempty"`
}

type TestSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type FailureContext struct {
	FailedJob      string                `json:"failed_job"`
	LogExcerpt     string                `json:"log_excerpt"`
	Classification FailureClassification `json:"classification"`
	CanRetry       bool                  `json:"can_retry"`
}
