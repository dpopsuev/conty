package driven

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRetryAfter = 60 * time.Second

	headerRateLimitLimit     = "X-RateLimit-Limit"
	headerRateLimitRemaining = "X-RateLimit-Remaining"
	headerRateLimitReset     = "X-RateLimit-Reset"

	headerRateLimitLimitAlt     = "RateLimit-Limit"
	headerRateLimitRemainingAlt = "RateLimit-Remaining"
	headerRateLimitResetAlt     = "RateLimit-Reset"
)

type RateLimitError struct {
	Backend    string
	RetryAfter time.Duration
	Limit      int
	Remaining  int
	Reset      time.Time
	Message    string
}

func (e *RateLimitError) Error() string {
	msg := fmt.Sprintf("rate limit exceeded for %s", e.Backend)

	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(", retry after %v", e.RetryAfter.Round(time.Second))
	}

	if e.Limit > 0 {
		msg += fmt.Sprintf(" (limit: %d", e.Limit)
		if e.Remaining >= 0 {
			msg += fmt.Sprintf(", remaining: %d", e.Remaining)
		}
		msg += ")"
	}

	if !e.Reset.IsZero() {
		msg += fmt.Sprintf(", resets at %s", e.Reset.Format(time.RFC3339))
	}

	if e.Message != "" {
		msg += fmt.Sprintf(": %s", e.Message)
	}

	return msg
}

func ParseRetryAfter(header string) time.Duration {
	if header == "" {
		return defaultRetryAfter
	}

	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	if t, err := http.ParseTime(header); err == nil {
		duration := time.Until(t)
		if duration > 0 {
			return duration
		}
	}

	return defaultRetryAfter
}

func ParseRateLimitHeaders(headers http.Header) (limit, remaining int, reset time.Time) {
	if limitStr := headers.Get(headerRateLimitLimit); limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}
	if remainingStr := headers.Get(headerRateLimitRemaining); remainingStr != "" {
		remaining, _ = strconv.Atoi(remainingStr)
	}
	if resetStr := headers.Get(headerRateLimitReset); resetStr != "" {
		if resetTimestamp, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			reset = time.Unix(resetTimestamp, 0)
		}
	}

	if limit == 0 {
		if limitStr := headers.Get(headerRateLimitLimitAlt); limitStr != "" {
			limit, _ = strconv.Atoi(limitStr)
		}
	}
	if remaining == 0 {
		if remainingStr := headers.Get(headerRateLimitRemainingAlt); remainingStr != "" {
			remaining, _ = strconv.Atoi(remainingStr)
		}
	}
	if reset.IsZero() {
		if resetStr := headers.Get(headerRateLimitResetAlt); resetStr != "" {
			if t, err := time.Parse(time.RFC3339, resetStr); err == nil {
				reset = t
			}
		}
	}

	return limit, remaining, reset
}
