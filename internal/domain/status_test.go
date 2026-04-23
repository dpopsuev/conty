package domain_test

import (
	"testing"

	"github.com/DanyPops/conty/internal/domain"
)

func TestRunStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   domain.RunStatus
		terminal bool
	}{
		{domain.RunStatusPending, false},
		{domain.RunStatusRunning, false},
		{domain.RunStatusSuccess, true},
		{domain.RunStatusFailure, true},
		{domain.RunStatusAborted, true},
		{domain.RunStatusNotFound, false},
	}
	for _, tt := range tests {
		if got := tt.status.IsTerminal(); got != tt.terminal {
			t.Errorf("RunStatus(%q).IsTerminal() = %v, want %v", tt.status, got, tt.terminal)
		}
	}
}
