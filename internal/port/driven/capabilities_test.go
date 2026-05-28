package driven_test

import (
	"context"
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
)

// ---- minimal fakes for type-assertion testing ----

type fakeCore struct{ name string }

func (f *fakeCore) Name() string                    { return f.name }
func (f *fakeCore) Type() string                    { return "fake" }
func (f *fakeCore) Capabilities() driven.CapabilitySet { return 0 }
func (f *fakeCore) GetRun(_ context.Context, _, _ string) (*domain.CIRun, error) {
	return nil, nil
}
func (f *fakeCore) SearchRuns(_ context.Context, _ string, _ domain.BuildFilter) ([]domain.CIRun, error) {
	return nil, nil
}
func (f *fakeCore) GetLog(_ context.Context, _, _ string, _ domain.LogFilter) (string, error) {
	return "", nil
}
func (f *fakeCore) CancelRun(_ context.Context, _, _ string) error { return nil }

type fakeTriggerable struct{ fakeCore }

func (f *fakeTriggerable) Trigger(_ context.Context, _ string, _ map[string]string) (*domain.TriggerReceipt, error) {
	return &domain.TriggerReceipt{}, nil
}
func (f *fakeTriggerable) ResolveReceipt(_ context.Context, r *domain.TriggerReceipt) (*domain.TriggerReceipt, error) {
	return r, nil
}
func (f *fakeTriggerable) EstimateDuration(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (f *fakeTriggerable) Capabilities() driven.CapabilitySet { return driven.CapTrigger }

type fakeWrapper struct {
	fakeCore
	inner driven.CICore
}

func (w *fakeWrapper) Unwrap() driven.CICore { return w.inner }

// ---- CapabilitySet ----

func TestCapabilitySet_Has(t *testing.T) {
	caps := driven.CapTrigger | driven.CapArtifacts
	cases := []struct {
		cap  driven.CapabilitySet
		want bool
	}{
		{driven.CapTrigger, true},
		{driven.CapArtifacts, true},
		{driven.CapChain, false},
		{driven.CapStages, false},
		{driven.CapHistory, false},
	}
	for _, tt := range cases {
		if got := caps.Has(tt.cap); got != tt.want {
			t.Errorf("Has(%v) = %v, want %v", tt.cap, got, tt.want)
		}
	}
}

func TestCapabilitySet_String_NonEmpty(t *testing.T) {
	caps := driven.CapTrigger | driven.CapArtifacts
	if s := caps.String(); s == "" {
		t.Error("String() should return non-empty for set capabilities")
	}
}

func TestCapabilitySet_Zero_String(t *testing.T) {
	var caps driven.CapabilitySet
	if s := caps.String(); s == "" {
		t.Error("zero CapabilitySet String() should return some representation (e.g. 'none')")
	}
}

// ---- As[T] ----

func TestAs_DirectImplementation(t *testing.T) {
	inner := &fakeTriggerable{}
	got, ok := driven.As[driven.CITriggerable](inner)
	if !ok {
		t.Fatal("expected As[CITriggerable] = true for fakeTriggerable")
	}
	if got == nil {
		t.Fatal("returned value should not be nil")
	}
}

func TestAs_PeelsThroughOneWrapper(t *testing.T) {
	inner := &fakeTriggerable{}
	wrapper := &fakeWrapper{inner: inner}
	got, ok := driven.As[driven.CITriggerable](wrapper)
	if !ok {
		t.Fatal("expected As[CITriggerable] = true after peeling wrapper")
	}
	if got == nil {
		t.Fatal("returned value should not be nil")
	}
}

func TestAs_ReturnsFalseWhenInnerDoesNotImplement(t *testing.T) {
	// fakeCore does not implement CIChainable
	inner := &fakeCore{}
	wrapper := &fakeWrapper{inner: inner}
	_, ok := driven.As[driven.CIChainable](wrapper)
	if ok {
		t.Error("expected As[CIChainable] = false — inner is plain fakeCore")
	}
}

func TestAs_NilReturnsZeroFalse(t *testing.T) {
	_, ok := driven.As[driven.CITriggerable](nil)
	if ok {
		t.Error("expected As(nil) = (zero, false)")
	}
}
