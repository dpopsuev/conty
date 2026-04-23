package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/DanyPops/conty/internal/adapter/driven/cache"
	"github.com/DanyPops/conty/internal/domain"
	"github.com/DanyPops/conty/internal/port/driven/driventest"
)

func TestCache_PollRunCachesTerminal(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Run = &domain.CIRun{
		ID:     "42",
		Status: domain.RunStatusSuccess,
		Result: domain.RunResultSuccess,
	}

	cached := cache.New(stub)
	ctx := context.Background()

	run1, err := cached.PollRun(ctx, "job", "42")
	if err != nil {
		t.Fatal(err)
	}
	if run1.ID != "42" {
		t.Fatalf("got %s, want 42", run1.ID)
	}

	run2, err := cached.PollRun(ctx, "job", "42")
	if err != nil {
		t.Fatal(err)
	}
	if run2.ID != "42" {
		t.Fatalf("got %s, want 42", run2.ID)
	}

	if len(stub.PollRunCalls) != 1 {
		t.Errorf("PollRun called %d times, want 1 (terminal result should be cached)", len(stub.PollRunCalls))
	}
}

func TestCache_PollRunDoesNotCacheRunning(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Run = &domain.CIRun{
		ID:     "42",
		Status: domain.RunStatusRunning,
	}

	cached := cache.New(stub, cache.WithPollTTL(1*time.Millisecond))
	ctx := context.Background()

	_, _ = cached.PollRun(ctx, "job", "42")
	time.Sleep(2 * time.Millisecond)
	_, _ = cached.PollRun(ctx, "job", "42")

	if len(stub.PollRunCalls) != 2 {
		t.Errorf("PollRun called %d times, want 2 (running should expire quickly)", len(stub.PollRunCalls))
	}
}

func TestCache_TriggerRunInvalidatesPolls(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.RunID = "new-run"
	stub.Run = &domain.CIRun{
		ID:     "42",
		Status: domain.RunStatusSuccess,
		Result: domain.RunResultSuccess,
	}

	cached := cache.New(stub)
	ctx := context.Background()

	_, _ = cached.PollRun(ctx, "job", "42")
	_, _ = cached.TriggerRun(ctx, "job", nil)
	_, _ = cached.PollRun(ctx, "job", "42")

	if len(stub.PollRunCalls) != 2 {
		t.Errorf("PollRun called %d times, want 2 (trigger should invalidate cache)", len(stub.PollRunCalls))
	}
}

func TestCache_ListJobsCached(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Jobs = []domain.CIJob{{ID: "j1", Name: "build"}}

	cached := cache.New(stub)
	ctx := context.Background()

	_, _ = cached.ListJobs(ctx, "job", "42")
	_, _ = cached.ListJobs(ctx, "job", "42")

	if len(stub.ListJobsCalls) != 1 {
		t.Errorf("ListJobs called %d times, want 1 (should be cached)", len(stub.ListJobsCalls))
	}
}

func TestCache_GetJobLogCached(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Log = "build output"

	cached := cache.New(stub)
	ctx := context.Background()

	_, _ = cached.GetJobLog(ctx, "job", "42")
	_, _ = cached.GetJobLog(ctx, "job", "42")

	if len(stub.GetJobLogCalls) != 1 {
		t.Errorf("GetJobLog called %d times, want 1 (should be cached)", len(stub.GetJobLogCalls))
	}
}
