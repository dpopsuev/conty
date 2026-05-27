package domain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
)

func TestCIRunUpstreamFields(t *testing.T) {
	t.Run("upstream fields marshal correctly", func(t *testing.T) {
		run := domain.CIRun{
			ID:            "6527",
			UpstreamJob:   "CI/far-edge-vran-deployment",
			UpstreamRunID: "17913",
		}
		data, err := json.Marshal(run)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(data)
		for _, want := range []string{`"upstream_job"`, `"upstream_run_id"`, "CI/far-edge-vran-deployment", "17913"} {
			if !strings.Contains(s, want) {
				t.Errorf("expected %q in JSON, got %s", want, s)
			}
		}
	})

	t.Run("zero-value upstream fields are omitted", func(t *testing.T) {
		run := domain.CIRun{ID: "1"}
		data, err := json.Marshal(run)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(data)
		for _, absent := range []string{"upstream_job", "upstream_run_id"} {
			if strings.Contains(s, absent) {
				t.Errorf("expected %q absent from zero-value JSON, got %s", absent, s)
			}
		}
	})

	t.Run("upstream fields round-trip unmarshal", func(t *testing.T) {
		original := domain.CIRun{
			ID:            "6527",
			UpstreamJob:   "CI/far-edge-vran-deployment",
			UpstreamRunID: "17913",
		}
		data, _ := json.Marshal(original)
		var decoded domain.CIRun
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.UpstreamJob != original.UpstreamJob {
			t.Errorf("UpstreamJob = %q, want %q", decoded.UpstreamJob, original.UpstreamJob)
		}
		if decoded.UpstreamRunID != original.UpstreamRunID {
			t.Errorf("UpstreamRunID = %q, want %q", decoded.UpstreamRunID, original.UpstreamRunID)
		}
	})
}
