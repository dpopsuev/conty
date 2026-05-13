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

func TestTriggerRedeploy_WithParams(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)

	params := map[string]string{"OPENSHIFT_RELEASE_IMAGE": "quay.io/ocp/release:4.22-nightly"}
	_, err := svc.TriggerRedeployWithParams(context.Background(), "test", "ocp-baremetal-ipi-deployment", params)
	if err != nil {
		t.Fatalf("TriggerRedeployWithParams: %v", err)
	}
	if len(stub.TriggerRunCalls) != 1 {
		t.Fatalf("TriggerRun called %d times, want 1", len(stub.TriggerRunCalls))
	}
	call := stub.TriggerRunCalls[0]
	if call.Params["OPENSHIFT_RELEASE_IMAGE"] != "quay.io/ocp/release:4.22-nightly" {
		t.Errorf("params = %v, want OPENSHIFT_RELEASE_IMAGE set", call.Params)
	}
}

func TestTriggerRedeploy_NilParamsFallsBack(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)

	_, err := svc.TriggerRedeployWithParams(context.Background(), "test", "some-job", nil)
	if err != nil {
		t.Fatalf("TriggerRedeployWithParams: %v", err)
	}
	if stub.TriggerRunCalls[0].Params != nil {
		t.Errorf("expected nil params, got %v", stub.TriggerRunCalls[0].Params)
	}
}

func TestCITrigger_WithParams(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)
	stub.QueueID = "build-99"

	params := map[string]string{"OPENSHIFT_RELEASE_IMAGE": "quay.io/ocp/release:4.22-nightly"}
	result, err := svc.CITrigger(context.Background(), "test", "ocp-baremetal-ipi-deployment", params)
	if err != nil {
		t.Fatalf("CITrigger: %v", err)
	}
	if result.QueueID == "" {
		t.Error("expected queue_id")
	}
	if result.BuildNumber == "" {
		t.Error("expected build_number from queue resolution")
	}
	if stub.TriggerRunCalls[0].Params["OPENSHIFT_RELEASE_IMAGE"] != "quay.io/ocp/release:4.22-nightly" {
		t.Errorf("params not passed through")
	}
}

func TestCIParams_ReturnsBuildParameters(t *testing.T) {
	stub := stubAdapter()
	stub.BuildParams = map[string]string{
		"OPENSHIFT_RELEASE_IMAGE": "quay.io/ocp/release:4.22-ec.5",
		"CLUSTER_NAME":           "kni-qe-79",
	}
	svc := app.NewService(stub)

	params, err := svc.CIParams(context.Background(), "test", "ocp-baremetal-ipi-deployment", "40201")
	if err != nil {
		t.Fatalf("CIParams: %v", err)
	}
	if params["OPENSHIFT_RELEASE_IMAGE"] != "quay.io/ocp/release:4.22-ec.5" {
		t.Errorf("missing OPENSHIFT_RELEASE_IMAGE, got %v", params)
	}
	if params["CLUSTER_NAME"] != "kni-qe-79" {
		t.Errorf("missing CLUSTER_NAME, got %v", params)
	}
}

func TestCIHistory_ReturnsBuilds(t *testing.T) {
	stub := stubAdapter()
	stub.Builds = []domain.CIRun{
		{ID: "40230", Status: domain.RunStatusSuccess},
		{ID: "40228", Status: domain.RunStatusFailure},
	}
	svc := app.NewService(stub)

	builds, err := svc.CIHistory(context.Background(), "test", "ocp-baremetal-ipi-deployment", 10)
	if err != nil {
		t.Fatalf("CIHistory: %v", err)
	}
	if len(builds) != 2 {
		t.Fatalf("got %d builds, want 2", len(builds))
	}
}

func TestCILog_ReturnsBuildLog(t *testing.T) {
	stub := stubAdapter()
	stub.Log = "ERROR: rhcos.json returned 404"
	svc := app.NewService(stub)

	log, err := svc.CILog(context.Background(), "test", "ocp-baremetal-ipi-deployment", "40232")
	if err != nil {
		t.Fatalf("CILog: %v", err)
	}
	if log == "" {
		t.Error("expected log output")
	}
}

func TestCIPoll_ResolvesQueueToBuild(t *testing.T) {
	stub := stubAdapter()
	stub.QueueID = "99"
	svc := app.NewService(stub)

	buildNum, err := svc.CIPoll(context.Background(), "test", "1256514")
	if err != nil {
		t.Fatalf("CIPoll: %v", err)
	}
	if buildNum == "" {
		t.Error("expected resolved build number")
	}
}

func TestOwnership_CITriggerRecordsOwnership(t *testing.T) {
	stub := stubAdapter()
	stub.QueueID = "build-42"
	svc := app.NewService(stub)

	result, err := svc.CITrigger(context.Background(), "test", "my-job", nil)
	if err != nil {
		t.Fatalf("CITrigger: %v", err)
	}

	if !svc.OwnsRun("test", result.BuildNumber) {
		t.Error("expected service to own the triggered build")
	}
	if svc.OwnsRun("test", "99999") {
		t.Error("should not own a build it didn't trigger")
	}
}

func TestOwnership_RedeployRecordsOwnership(t *testing.T) {
	stub := stubAdapter()
	stub.QueueID = "build-55"
	svc := app.NewService(stub)

	runID, err := svc.TriggerRedeployWithParams(context.Background(), "test", "my-job", nil)
	if err != nil {
		t.Fatalf("TriggerRedeployWithParams: %v", err)
	}
	if !svc.OwnsRun("test", runID) {
		t.Error("expected service to own the redeployed build")
	}
}

func TestOwnership_ListOwnedRuns(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)

	stub.QueueID = "build-1"
	svc.CITrigger(context.Background(), "test", "job-a", nil)
	stub.QueueID = "build-2"
	svc.CITrigger(context.Background(), "test", "job-b", nil)

	owned := svc.ListOwnedRuns()
	if len(owned) != 2 {
		t.Fatalf("ListOwnedRuns() = %d, want 2", len(owned))
	}
}

func TestWatcher_CITriggerReturnsEstimatedDuration(t *testing.T) {
	stub := stubAdapter()
	stub.QueueID = "build-42"
	stub.Run.Duration = 7200000
	stub.EstimatedDuration = 7200000
	svc := app.NewService(stub)

	result, err := svc.CITrigger(context.Background(), "test", "my-job", nil)
	if err != nil {
		t.Fatalf("CITrigger: %v", err)
	}
	if result.EstimatedDuration == 0 {
		t.Error("expected estimated_duration from trigger result")
	}
	if result.PollInterval == 0 {
		t.Error("expected poll_interval (5% of estimated)")
	}
}

func TestWatcher_CIWatch(t *testing.T) {
	stub := stubAdapter()
	stub.Run = &domain.CIRun{
		ID:       "42",
		Status:   domain.RunStatusRunning,
		Duration: 3600000,
	}
	stub.EstimatedDuration = 7200000
	svc := app.NewService(stub)

	status, err := svc.CIWatch(context.Background(), "test", "my-job", "42")
	if err != nil {
		t.Fatalf("CIWatch: %v", err)
	}
	if status.Progress == 0 {
		t.Error("expected progress > 0")
	}
	if status.Overdue {
		t.Error("50% elapsed should not be overdue")
	}
}

func TestWatcher_CIWatch_Overdue(t *testing.T) {
	stub := stubAdapter()
	stub.Run = &domain.CIRun{
		ID:       "42",
		Status:   domain.RunStatusRunning,
		Duration: 12000000,
	}
	stub.EstimatedDuration = 7200000
	svc := app.NewService(stub)

	status, err := svc.CIWatch(context.Background(), "test", "my-job", "42")
	if err != nil {
		t.Fatalf("CIWatch: %v", err)
	}
	if !status.Overdue {
		t.Error("167% elapsed should be overdue")
	}
}

func TestBackendNotFound(t *testing.T) {
	svc := app.NewService()
	_, err := svc.CheckLatest(context.Background(), "nonexistent", "job")
	if !errors.Is(err, app.ErrBackendNotFound) {
		t.Errorf("err = %v, want ErrBackendNotFound", err)
	}
}

func TestListBackends_ReturnsRegisteredNames(t *testing.T) {
	stub1 := driventest.NewStubCIAdapter("jenkins-ci")
	stub2 := driventest.NewStubCIAdapter("jenkins-auto")
	svc := app.NewService(stub1, stub2)

	backends := svc.ListBackends()
	if len(backends) != 2 {
		t.Fatalf("ListBackends() = %d, want 2", len(backends))
	}
	found := map[string]bool{}
	for _, b := range backends {
		found[b] = true
	}
	if !found["jenkins-ci"] || !found["jenkins-auto"] {
		t.Errorf("backends = %v, want jenkins-ci and jenkins-auto", backends)
	}
}

func TestListBackends_EmptyWhenNone(t *testing.T) {
	svc := app.NewService()
	backends := svc.ListBackends()
	if len(backends) != 0 {
		t.Errorf("ListBackends() = %v, want empty", backends)
	}
}

func TestCIArtifacts_ListAndGet(t *testing.T) {
	stub := stubAdapter()
	stub.Artifacts = []domain.CIArtifact{
		{Name: "report.xml", Path: "artifacts/report.xml", Size: 1024},
		{Name: "log.txt", Path: "artifacts/log.txt", Size: 512},
	}
	stub.Artifact = []byte("<xml>test report</xml>")
	svc := app.NewService(stub)

	artifacts, err := svc.CIArtifacts(context.Background(), "test", "my-job", "42")
	if err != nil {
		t.Fatalf("CIArtifacts: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("got %d artifacts, want 2", len(artifacts))
	}

	data, err := svc.CIArtifactGet(context.Background(), "test", "my-job", "42", "artifacts/report.xml")
	if err != nil {
		t.Fatalf("CIArtifactGet: %v", err)
	}
	if string(data) != "<xml>test report</xml>" {
		t.Errorf("artifact content = %q", string(data))
	}
}

func TestBackendInfo_ReturnsDetails(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)

	info := svc.BackendInfo()
	if len(info) != 1 {
		t.Fatalf("BackendInfo() = %d, want 1", len(info))
	}
	if info[0].Name != "test" {
		t.Errorf("name = %s, want test", info[0].Name)
	}
}

func TestCICancel_OwnsRun(t *testing.T) {
	stub := stubAdapter()
	stub.QueueID = "build-77"
	svc := app.NewService(stub)

	result, err := svc.CITrigger(context.Background(), "test", "my-job", nil)
	if err != nil {
		t.Fatalf("CITrigger: %v", err)
	}

	if err := svc.CICancel(context.Background(), "test", "my-job", result.BuildNumber); err != nil {
		t.Errorf("CICancel owned run: %v", err)
	}
}

func TestCICancel_NotOwned(t *testing.T) {
	stub := stubAdapter()
	svc := app.NewService(stub)

	err := svc.CICancel(context.Background(), "test", "my-job", "99999")
	if err == nil {
		t.Error("expected error cancelling unowned run")
	}
}

func TestGetVerdict_FailureClassification(t *testing.T) {
	stub := stubAdapter()
	stub.Run = &domain.CIRun{
		ID:     "run-1",
		Status: domain.RunStatusFailure,
		Result: domain.RunResultFailure,
	}
	stub.Log = "error: connection timed out after 30s connecting to API server"
	svc := app.NewService(stub)

	verdict, err := svc.GetVerdict(context.Background(), "test", "my-job")
	if err != nil {
		t.Fatalf("GetVerdict: %v", err)
	}
	if verdict.Failure == nil {
		t.Fatal("expected failure context")
	}
	if verdict.Failure.Classification != domain.FailureNetwork {
		t.Errorf("classification = %q, want network_timeout", verdict.Failure.Classification)
	}
	if !verdict.Failure.CanRetry {
		t.Error("network timeout should be retryable")
	}
}

func TestBackendInfo_ReturnsActualType(t *testing.T) {
	stub := driventest.NewStubCIAdapter("jenkins-auto")
	svc := app.NewService(stub)

	info := svc.BackendInfo()
	if len(info) != 1 {
		t.Fatalf("got %d backends, want 1", len(info))
	}
	if info[0].Type != "stub" {
		t.Errorf("type = %q, want stub (StubCIAdapter.Type())", info[0].Type)
	}
}
