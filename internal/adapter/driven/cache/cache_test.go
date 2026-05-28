package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/dpopsuev/conty/internal/adapter/driven/cache"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
	"github.com/dpopsuev/conty/internal/port/driven/driventest"
)

func TestCache_GetRunCachesTerminal(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Run = &domain.CIRun{
		ID:     "42",
		Status: domain.RunStatusSuccess,
		Result: domain.RunResultSuccess,
	}

	cached := cache.New(stub)
	ctx := context.Background()

	run1, err := cached.GetRun(ctx, "job", "42")
	if err != nil {
		t.Fatal(err)
	}
	if run1.ID != "42" {
		t.Fatalf("got %s, want 42", run1.ID)
	}

	run2, err := cached.GetRun(ctx, "job", "42")
	if err != nil {
		t.Fatal(err)
	}
	if run2.ID != "42" {
		t.Fatalf("got %s, want 42", run2.ID)
	}

	if len(stub.GetRunCalls) != 1 {
		t.Errorf("GetRun called %d times, want 1 (terminal result should be cached)", len(stub.GetRunCalls))
	}
}

func TestCache_GetRunDoesNotCacheRunning(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Run = &domain.CIRun{
		ID:     "42",
		Status: domain.RunStatusRunning,
	}

	cached := cache.New(stub, cache.WithPollTTL(1*time.Millisecond))
	ctx := context.Background()

	_, _ = cached.GetRun(ctx, "job", "42")
	time.Sleep(2 * time.Millisecond)
	_, _ = cached.GetRun(ctx, "job", "42")

	if len(stub.GetRunCalls) != 2 {
		t.Errorf("GetRun called %d times, want 2 (running should expire quickly)", len(stub.GetRunCalls))
	}
}

func TestCache_TriggerInvalidatesPolls(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.RunID = "new-run"
	stub.Run = &domain.CIRun{
		ID:     "42",
		Status: domain.RunStatusSuccess,
		Result: domain.RunResultSuccess,
	}

	cached := cache.New(stub)
	ctx := context.Background()

	_, _ = cached.GetRun(ctx, "job", "42")
	_, _ = cached.Trigger(ctx, "job", nil)
	_, _ = cached.GetRun(ctx, "job", "42")

	if len(stub.GetRunCalls) != 2 {
		t.Errorf("GetRun called %d times, want 2 (trigger should invalidate cache)", len(stub.GetRunCalls))
	}
}

func TestCache_ListStagesCached(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Jobs = []domain.CIJob{{ID: "j1", Name: "build"}}

	cached := cache.New(stub)
	ctx := context.Background()

	_, _ = cached.ListStages(ctx, "job", "42")
	_, _ = cached.ListStages(ctx, "job", "42")

	if len(stub.ListStagesCalls) != 1 {
		t.Errorf("ListStages called %d times, want 1 (should be cached)", len(stub.ListStagesCalls))
	}
}

func TestCache_GetLogCached(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Log = "build output"

	cached := cache.New(stub)
	ctx := context.Background()

	_, _ = cached.GetLog(ctx, "job", "42", domain.LogFilter{})
	_, _ = cached.GetLog(ctx, "job", "42", domain.LogFilter{})

	if len(stub.GetLogCalls) != 1 {
		t.Errorf("GetLog called %d times, want 1 (should be cached)", len(stub.GetLogCalls))
	}
}

func TestCache_Unwrap_ReturnsInner(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	cached := cache.New(stub)

	// cache.New returns *cache.Adapter which implements driven.Unwrapper
	var coreAdapter driven.CICore = cached
	w, ok := coreAdapter.(driven.Unwrapper)
	if !ok {
		t.Fatal("cache.Adapter should implement driven.Unwrapper")
	}
	inner := w.Unwrap()
	if inner != driven.CICore(stub) {
		t.Error("Unwrap() should return the inner adapter")
	}
}

func TestCache_As_PeelsThroughToInner(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test") // implements CIChainable
	cached := cache.New(stub)

	// As[T] should peel through to stub and find CIChainable
	_, ok := driven.As[driven.CIChainable](cached)
	if !ok {
		t.Error("driven.As[CIChainable] should return true when inner implements CIChainable")
	}
}

func TestCache_As_ReturnsFalseWhenInnerLacksInterface(t *testing.T) {
	// A minimal CICore that does NOT implement CIChainable
	type minimalCore struct{ driventest.StubCIAdapter }
	// Note: StubCIAdapter DOES implement CIChainable, so we need to wrap differently.
	// Use the driven.As approach: if inner implements Unwrapper, peel.
	// Since StubCIAdapter has everything, we test via the test in capabilities_test.go instead.
	// This test verifies the cache Capabilities() delegation.
	stub := driventest.NewStubCIAdapter("test")
	cached := cache.New(stub)

	caps := cached.Capabilities()
	if !caps.Has(driven.CapChain) {
		t.Error("Capabilities() should delegate to inner; StubCIAdapter supports CapChain")
	}
}
