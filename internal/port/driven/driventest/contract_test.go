package driventest_test

import (
	"errors"
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
	"github.com/dpopsuev/conty/internal/port/driven/driventest"
)

func TestStubCIAdapter_ContractCompliance(t *testing.T) {
	driventest.RunCIAdapterContractTests(t, func(t *testing.T) (driven.CIAdapter, string, string) {
		stub := driventest.NewStubCIAdapter("stub")
		stub.RunID = "run-42"
		stub.Run = &domain.CIRun{
			ID:     "run-42",
			Name:   "test-job",
			Status: domain.RunStatusSuccess,
			Result: domain.RunResultSuccess,
		}
		stub.Jobs = []domain.CIJob{
			{ID: "j1", Name: "stage-1", Status: domain.RunStatusSuccess},
		}
		stub.Log = "Build succeeded"
		stub.Artifacts = []domain.CIArtifact{
			{Name: "report.xml", Path: "artifacts/report.xml"},
		}
		stub.GetArtifactErr = errors.New("artifact not found")
		return stub, "test-job", "run-42"
	})
}
