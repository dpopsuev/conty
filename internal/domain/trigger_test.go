package domain_test

import (
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
)

func TestTriggerReceipt_ImmediateResolution(t *testing.T) {
	r := domain.TriggerReceipt{RunID: "42", NeedsResolve: false, Backend: "gitlab", JobRef: "main"}
	if r.NeedsResolve {
		t.Error("NeedsResolve should be false when RunID is already populated")
	}
	if r.RunID != "42" {
		t.Errorf("RunID = %q, want 42", r.RunID)
	}
}

func TestTriggerReceipt_QueueBasedResolution(t *testing.T) {
	r := domain.TriggerReceipt{OpaqueRef: "1234", NeedsResolve: true, Backend: "jenkins"}
	if !r.NeedsResolve {
		t.Error("NeedsResolve should be true when waiting for queue resolution")
	}
	if r.RunID != "" {
		t.Errorf("RunID should be empty for unresolved receipt, got %q", r.RunID)
	}
	if r.OpaqueRef != "1234" {
		t.Errorf("OpaqueRef = %q, want 1234", r.OpaqueRef)
	}
}

func TestTriggerReceipt_ZeroValue(t *testing.T) {
	var r domain.TriggerReceipt
	if r.NeedsResolve || r.RunID != "" || r.OpaqueRef != "" {
		t.Error("zero-value TriggerReceipt should have all empty/false fields")
	}
}
