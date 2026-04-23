package driven_test

import (
	"net/http"
	"testing"
	"time"

	adapterdriven "github.com/DanyPops/conty/internal/adapter/driven"
)

func TestRateLimitError_Message(t *testing.T) {
	err := &adapterdriven.RateLimitError{
		Backend:    "jenkins",
		RetryAfter: 60 * time.Second,
		Limit:      100,
		Remaining:  0,
		Message:    "slow down",
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("empty error message")
	}
	for _, want := range []string{"jenkins", "retry after", "100", "slow down"} {
		if !contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
}

func TestParseRetryAfter_Seconds(t *testing.T) {
	d := adapterdriven.ParseRetryAfter("30")
	if d != 30*time.Second {
		t.Errorf("got %v, want 30s", d)
	}
}

func TestParseRetryAfter_Empty(t *testing.T) {
	d := adapterdriven.ParseRetryAfter("")
	if d != 60*time.Second {
		t.Errorf("got %v, want 60s (default)", d)
	}
}

func TestParseRateLimitHeaders_GitHub(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", "4999")
	h.Set("X-RateLimit-Reset", "1700000000")

	limit, remaining, reset := adapterdriven.ParseRateLimitHeaders(h)
	if limit != 5000 {
		t.Errorf("limit = %d, want 5000", limit)
	}
	if remaining != 4999 {
		t.Errorf("remaining = %d, want 4999", remaining)
	}
	if reset.IsZero() {
		t.Error("reset should not be zero")
	}
}

func TestParseRateLimitHeaders_GitLab(t *testing.T) {
	h := http.Header{}
	h.Set("RateLimit-Limit", "600")
	h.Set("RateLimit-Remaining", "599")
	h.Set("RateLimit-Reset", "2026-04-23T18:00:00Z")

	limit, remaining, reset := adapterdriven.ParseRateLimitHeaders(h)
	if limit != 600 {
		t.Errorf("limit = %d, want 600", limit)
	}
	if remaining != 599 {
		t.Errorf("remaining = %d, want 599", remaining)
	}
	if reset.IsZero() {
		t.Error("reset should not be zero")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
