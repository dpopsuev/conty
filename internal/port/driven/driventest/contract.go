package driventest

import (
	"context"
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

func RunCIAdapterContractTests(t *testing.T, setup func(t *testing.T) (driven.CICore, string, string)) {
	t.Helper()

	t.Run("Name", func(t *testing.T) {
		adapter, _, _ := setup(t)
		if name := adapter.Name(); name == "" {
			t.Error("Name() returned empty string")
		}
	})

	t.Run("Capabilities", func(t *testing.T) {
		adapter, _, _ := setup(t)
		caps := adapter.Capabilities()
		_ = caps.String() // must not panic
	})

	t.Run("GetRun", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		run, err := adapter.GetRun(context.Background(), jobName, runID)
		if err != nil {
			t.Fatalf("GetRun(%q, %q): %v", jobName, runID, err)
		}
		if run == nil {
			t.Fatal("GetRun returned nil")
		}
		if run.ID == "" {
			t.Error("run.ID is empty")
		}
	})

	t.Run("GetLog", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		log, err := adapter.GetLog(context.Background(), jobName, runID, domain.LogFilter{})
		if err != nil {
			t.Fatalf("GetLog(%q, %q): %v", jobName, runID, err)
		}
		_ = log
	})

	t.Run("ListStages_optional", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		p, ok := adapter.(driven.CIPipeliner)
		if !ok {
			t.Logf("ListStages: backend does not implement CIPipeliner — skipping")
			return
		}
		stages, err := p.ListStages(context.Background(), jobName, runID)
		if err != nil {
			t.Logf("ListStages(%q, %q): %v (pipeline backends may not have stages)", jobName, runID, err)
			return
		}
		_ = stages
	})

	t.Run("ListArtifacts_optional", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		a, ok := adapter.(driven.CIArtifactStore)
		if !ok {
			t.Logf("ListArtifacts: backend does not implement CIArtifactStore — skipping")
			return
		}
		artifacts, err := a.ListArtifacts(context.Background(), jobName, runID)
		if err != nil {
			t.Fatalf("ListArtifacts(%q, %q): %v", jobName, runID, err)
		}
		_ = artifacts
	})

	t.Run("GetArtifact_NotFound_optional", func(t *testing.T) {
		adapter, jobName, runID := setup(t)
		a, ok := adapter.(driven.CIArtifactStore)
		if !ok {
			t.Logf("GetArtifact: backend does not implement CIArtifactStore — skipping")
			return
		}
		_, err := a.GetArtifact(context.Background(), jobName, runID, "nonexistent/path.txt")
		if err == nil {
			t.Error("GetArtifact(nonexistent) should return error")
		}
	})
}
