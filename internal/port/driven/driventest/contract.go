package driventest

import (
	"context"
	"testing"

	"github.com/DanyPops/conty/internal/port/driven"
)

func RunCIAdapterContractTests(t *testing.T, setup func(t *testing.T) (driven.CIAdapter, string, string)) {
	t.Helper()

	t.Run("Name", func(t *testing.T) {
		adapter, _, _ := setup(t)
		if name := adapter.Name(); name == "" {
			t.Error("Name() returned empty string")
		}
	})

	t.Run("TriggerRun", func(t *testing.T) {
		adapter, jobName, _ := setup(t)
		runID, err := adapter.TriggerRun(context.Background(), jobName, nil)
		if err != nil {
			t.Fatalf("TriggerRun(%q): %v", jobName, err)
		}
		if runID == "" {
			t.Error("TriggerRun returned empty run ID")
		}
	})

	t.Run("PollRun", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		run, err := adapter.PollRun(context.Background(), jobName, runID)
		if err != nil {
			t.Fatalf("PollRun(%q, %q): %v", jobName, runID, err)
		}
		if run == nil {
			t.Fatal("PollRun returned nil")
		}
		if run.ID == "" {
			t.Error("run.ID is empty")
		}
	})

	t.Run("ListJobs", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		jobs, err := adapter.ListJobs(context.Background(), jobName, runID)
		if err != nil {
			t.Fatalf("ListJobs(%q, %q): %v", jobName, runID, err)
		}
		_ = jobs
	})

	t.Run("GetJobLog", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		log, err := adapter.GetJobLog(context.Background(), jobName, runID)
		if err != nil {
			t.Fatalf("GetJobLog(%q, %q): %v", jobName, runID, err)
		}
		_ = log
	})

	t.Run("ListArtifacts", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		artifacts, err := adapter.ListArtifacts(context.Background(), jobName, runID)
		if err != nil {
			t.Fatalf("ListArtifacts(%q, %q): %v", jobName, runID, err)
		}
		_ = artifacts
	})

	t.Run("GetArtifact_NotFound", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		_, err := adapter.GetArtifact(context.Background(), jobName, runID, "nonexistent/path.txt")
		if err == nil {
			t.Error("GetArtifact(nonexistent) should return error")
		}
	})
}
