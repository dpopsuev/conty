package app

import (
	"strings"
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
)

func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%10+1))
		b.WriteByte('\n')
	}
	return b.String()
}

// --- grep ---

func TestApplyLogFilter_GrepSinglePattern(t *testing.T) {
	raw := "ok line\nFAILED here\nanother ok\nerror found\n"
	got := applyLogFilter(raw, domain.LogFilter{Grep: "FAILED"})
	if len(got.Lines) != 1 || !strings.Contains(got.Lines[0], "FAILED") {
		t.Errorf("expected 1 FAILED line, got %v", got.Lines)
	}
	if !got.Filtered {
		t.Error("Filtered should be true when grep is set")
	}
}

func TestApplyLogFilter_GrepCaseInsensitive(t *testing.T) {
	raw := "Error: something\nall good\n"
	got := applyLogFilter(raw, domain.LogFilter{Grep: "error"})
	if len(got.Lines) != 1 {
		t.Errorf("expected 1 line (case-insensitive), got %v", got.Lines)
	}
}

func TestApplyLogFilter_GrepPipeOR(t *testing.T) {
	// | must act as OR, not a literal pipe character.
	raw := "ok line\nFAILED: step 1\nfatal: crash\nerror: timeout\nall good\n"
	got := applyLogFilter(raw, domain.LogFilter{Grep: "FAILED|fatal|error"})
	if len(got.Lines) != 3 {
		t.Errorf("expected 3 lines matching FAILED|fatal|error, got %d: %v", len(got.Lines), got.Lines)
	}
}

func TestApplyLogFilter_GrepInvalidRegexFallsBackToLiteral(t *testing.T) {
	// An invalid regex pattern must not panic; it should fall back to literal substring match.
	raw := "line with [unclosed bracket\nnormal line\n"
	got := applyLogFilter(raw, domain.LogFilter{Grep: "[unclosed"})
	// Literal match: "[unclosed" is a substring of the first line.
	if len(got.Lines) != 1 {
		t.Errorf("expected 1 line on invalid-regex fallback, got %d: %v", len(got.Lines), got.Lines)
	}
}

// --- tail ---

func TestApplyLogFilter_DefaultTailNoGrep(t *testing.T) {
	// Without grep, default tail of 200 is applied.
	raw := lines(300)
	got := applyLogFilter(raw, domain.LogFilter{})
	if len(got.Lines) != domain.LogDefaultTail {
		t.Errorf("expected %d lines (default tail), got %d", domain.LogDefaultTail, len(got.Lines))
	}
	if !got.Truncated {
		t.Error("Truncated should be true when 300 lines are trimmed to 200")
	}
}

func TestApplyLogFilter_ExplicitTailNoGrep(t *testing.T) {
	raw := lines(100)
	got := applyLogFilter(raw, domain.LogFilter{Tail: 10})
	if len(got.Lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(got.Lines))
	}
}

func TestApplyLogFilter_TailMinusOneReturnsAll(t *testing.T) {
	raw := lines(300)
	got := applyLogFilter(raw, domain.LogFilter{Tail: -1})
	// 300 newline-terminated lines → split gives 301 elements with trailing empty.
	if got.Truncated {
		t.Error("Truncated should be false for tail=-1")
	}
}

// --- grep + tail interaction ---

func TestApplyLogFilter_GrepNoImplicitTail(t *testing.T) {
	// When grep is set and tail is 0 (unset), all matching lines must be returned —
	// the default 200-line tail must NOT silently discard grep results beyond 200.
	var b strings.Builder
	for i := 0; i < 250; i++ {
		b.WriteString("error line\n")
	}
	got := applyLogFilter(b.String(), domain.LogFilter{Grep: "error"})
	if len(got.Lines) != 250 {
		t.Errorf("expected 250 matching lines (no implicit tail), got %d", len(got.Lines))
	}
}

func TestApplyLogFilter_GrepWithExplicitTail(t *testing.T) {
	// Explicit tail limits matching lines from the end.
	raw := "error first\nerror second\nerror third\nerror fourth\n"
	got := applyLogFilter(raw, domain.LogFilter{Grep: "error", Tail: 2})
	if len(got.Lines) != 2 {
		t.Errorf("expected 2 lines (last 2 matches), got %d: %v", len(got.Lines), got.Lines)
	}
	if !strings.Contains(got.Lines[0], "third") || !strings.Contains(got.Lines[1], "fourth") {
		t.Errorf("expected last 2 matches, got %v", got.Lines)
	}
}

func TestApplyLogFilter_GrepBeforeTail(t *testing.T) {
	// Grep applies to the full log, not just the tail window.
	// A log where the only matching line is line 1 of 1000 must still be found.
	var b strings.Builder
	b.WriteString("FATAL: crash\n")
	for i := 0; i < 999; i++ {
		b.WriteString("normal line\n")
	}
	got := applyLogFilter(b.String(), domain.LogFilter{Grep: "FATAL", Tail: 50})
	if len(got.Lines) != 1 {
		t.Errorf("expected 1 line (grep before tail), got %d: %v", len(got.Lines), got.Lines)
	}
}
