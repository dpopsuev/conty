package domain

type TriggerResult struct {
	QueueID     string `json:"queue_id"`
	BuildNumber string `json:"build_number,omitempty"`
	JobRef      string `json:"job_ref"`
	Backend     string `json:"backend"`
}
