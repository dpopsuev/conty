package domain

// TriggerReceipt is returned by CITriggerable.Trigger. Backends that assign
// a run ID synchronously (GitLab, Prow) set NeedsResolve=false and populate
// RunID immediately. Backends that use an async queue (Jenkins) or have no
// run ID at dispatch time (GitHub Actions) set NeedsResolve=true and populate
// OpaqueRef with a backend-specific polling token. Call ResolveReceipt in a
// loop until NeedsResolve is false.
type TriggerReceipt struct {
	RunID        string // populated when run ID is known; empty until resolved
	OpaqueRef    string // backend-specific token: Jenkins queue ID, GitHub dispatch timestamp
	NeedsResolve bool   // true → call ResolveReceipt() until false
	Backend      string
	JobRef       string
}

type TriggerResult struct {
	QueueID           string `json:"queue_id"`
	BuildNumber       string `json:"build_number,omitempty"`
	JobRef            string `json:"job_ref"`
	Backend           string `json:"backend"`
	EstimatedDuration int64  `json:"estimated_duration,omitempty"`
	PollInterval      int64  `json:"poll_interval,omitempty"`
}

type WatchStatus struct {
	BuildNumber string    `json:"build_number"`
	JobRef      string    `json:"job_ref"`
	Backend     string    `json:"backend"`
	Status      RunStatus `json:"status"`
	Progress    float64   `json:"progress"`
	Elapsed     int64     `json:"elapsed"`
	Estimated   int64     `json:"estimated"`
	Overdue     bool      `json:"overdue"`
}

type OwnedRun struct {
	Backend     string `json:"backend"`
	JobRef      string `json:"job_ref"`
	BuildNumber string `json:"build_number"`
	QueueID     string `json:"queue_id"`
}
