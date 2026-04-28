package domain

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
