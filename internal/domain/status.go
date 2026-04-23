package domain

type RunStatus string

const (
	RunStatusPending  RunStatus = "pending"
	RunStatusRunning  RunStatus = "running"
	RunStatusSuccess  RunStatus = "success"
	RunStatusFailure  RunStatus = "failure"
	RunStatusAborted  RunStatus = "aborted"
	RunStatusNotFound RunStatus = "not_found"
)

func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunStatusSuccess, RunStatusFailure, RunStatusAborted:
		return true
	default:
		return false
	}
}

type RunResult string

const (
	RunResultSuccess  RunResult = "SUCCESS"
	RunResultFailure  RunResult = "FAILURE"
	RunResultUnstable RunResult = "UNSTABLE"
	RunResultAborted  RunResult = "ABORTED"
)

type FailureClassification string

const (
	FailureNetwork FailureClassification = "network_timeout"
	FailureConfig  FailureClassification = "config_error"
	FailureInfra   FailureClassification = "infra_failure"
	FailureTest    FailureClassification = "test_failure"
	FailureUnknown FailureClassification = "unknown"
)
