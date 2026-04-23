package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dpopsuev/conty/internal/app"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven/driventest"
)

func stubAdapter() *driventest.StubCIAdapter {
	stub := driventest.NewStubCIAdapter("test")
	stub.RunID = "run-1"
	stub.Run = &domain.CIRun{
		ID:     "run-1",
		Name:   "test-job",
		Status: domain.RunStatusSuccess,
		Result: domain.RunResultSuccess,
	}
	return stub
}

func testPipeline() domain.Pipeline {
	return domain.Pipeline{
		Name:    "test-pipeline",
		Backend: "test",
		Steps: []domain.PipelineStep{
			{JobName: "step-1"},
			{JobName: "step-2"},
			{JobName: "step-3"},
		},
	}
}

func TestTriggerPipeline_AllStepsSucceed(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)
	svc.RegisterPipeline(testPipeline())

	run, err := svc.TriggerPipeline(context.Background(), "test-pipeline")
	if err != nil {
		t.Fatalf("TriggerPipeline: %v", err)
	}
	if run.Status != domain.RunStatusSuccess {
		t.Errorf("status = %s, want success", run.Status)
	}
	if len(run.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(run.Steps))
	}
	for i, step := range run.Steps {
		if step.Status != domain.RunStatusSuccess {
			t.Errorf("step[%d] status = %s, want success", i, step.Status)
		}
	}
	if len(stub.TriggerRunCalls) != 3 {
		t.Errorf("TriggerRun called %d times, want 3", len(stub.TriggerRunCalls))
	}
}

func TestTriggerPipeline_StopsOnFailure(t *testing.T) {
	stub := stubAdapter()
	stub.Run = &domain.CIRun{
		ID:     "run-1",
		Name:   "step-2",
		Status: domain.RunStatusFailure,
		Result: domain.RunResultFailure,
	}
	svc := app.NewService(stub)
	svc.RegisterPipeline(testPipeline())

	run, err := svc.TriggerPipeline(context.Background(), "test-pipeline")
	if err != nil {
		t.Fatalf("TriggerPipeline: %v", err)
	}
	if run.Status != domain.RunStatusFailure {
		t.Errorf("status = %s, want failure", run.Status)
	}
	if run.Steps[0].Status != domain.RunStatusFailure {
		t.Errorf("step[0] should be failure (poll returns failure)")
	}
	if len(stub.TriggerRunCalls) != 1 {
		t.Errorf("TriggerRun called %d times, want 1 (should stop after first poll fails)", len(stub.TriggerRunCalls))
	}
}

func TestTriggerPipeline_TriggerError(t *testing.T) {
	stub := stubAdapter()
	stub.TriggerRunErr = errors.New("connection refused")
	svc := app.NewService(stub)
	svc.RegisterPipeline(testPipeline())

	run, err := svc.TriggerPipeline(context.Background(), "test-pipeline")
	if err != nil {
		t.Fatalf("TriggerPipeline should not error, got: %v", err)
	}
	if run.Status != domain.RunStatusFailure {
		t.Errorf("status = %s, want failure", run.Status)
	}
}

func TestTriggerPipeline_NotFound(t *testing.T) {
	svc := app.NewService()
	_, err := svc.TriggerPipeline(context.Background(), "nonexistent")
	if !errors.Is(err, app.ErrPipelineNotFound) {
		t.Errorf("err = %v, want ErrPipelineNotFound", err)
	}
}

func TestCheckLatest(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)

	check, err := svc.CheckLatest(context.Background(), "test", "CI/some-job")
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if check.Status != domain.RunStatusSuccess {
		t.Errorf("status = %s, want success", check.Status)
	}
	if check.Backend != "test" {
		t.Errorf("backend = %s, want test", check.Backend)
	}
	if check.JobRef != "CI/some-job" {
		t.Errorf("job_ref = %s, want CI/some-job", check.JobRef)
	}
}

func TestGetVerdict_Success(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)

	verdict, err := svc.GetVerdict(context.Background(), "test", "CI/some-job")
	if err != nil {
		t.Fatalf("GetVerdict: %v", err)
	}
	if verdict.TestSummary == nil {
		t.Error("expected test_summary on success")
	}
	if verdict.Failure != nil {
		t.Error("expected no failure on success")
	}
}

func TestGetVerdict_Failure(t *testing.T) {
	stub := stubAdapter()
	stub.Run = &domain.CIRun{
		ID:     "run-1",
		Name:   "CI/some-job",
		Status: domain.RunStatusFailure,
		Result: domain.RunResultFailure,
	}
	stub.Jobs = []domain.CIJob{
		{ID: "j1", Name: "deploy-spoke", Status: domain.RunStatusFailure},
	}
	stub.Log = "error: connection timed out"
	svc := app.NewService(stub)

	verdict, err := svc.GetVerdict(context.Background(), "test", "CI/some-job")
	if err != nil {
		t.Fatalf("GetVerdict: %v", err)
	}
	if verdict.Failure == nil {
		t.Fatal("expected failure context")
	}
	if verdict.Failure.FailedJob != "deploy-spoke" {
		t.Errorf("failed_job = %s, want deploy-spoke", verdict.Failure.FailedJob)
	}
}

func TestBackendNotFound(t *testing.T) {
	svc := app.NewService()
	_, err := svc.CheckLatest(context.Background(), "nonexistent", "job")
	if !errors.Is(err, app.ErrBackendNotFound) {
		t.Errorf("err = %v, want ErrBackendNotFound", err)
	}
}
