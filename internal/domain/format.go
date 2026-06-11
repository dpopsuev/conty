package domain

import (
	"fmt"
	"strings"
)

// FmtDuration converts milliseconds to a human-readable duration string.
// Examples: 0 → "0s", 90000 → "1m30s", 7450490 → "2h4m10s".
func FmtDuration(ms int64) string {
	s := ms / 1000
	if s == 0 {
		return "0s"
	}
	h := s / 3600
	s -= h * 3600
	m := s / 60
	s -= m * 60
	switch {
	case h > 0 && m > 0 && s > 0:
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0 && s > 0:
		return fmt.Sprintf("%dh%ds", h, s)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0 && s > 0:
		return fmt.Sprintf("%dm%ds", m, s)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// CleanStepDesc normalises a step description from the wfapi.
// Multi-line shell scripts are reduced to the first non-empty, non-whitespace line.
// Leading/trailing whitespace is trimmed throughout.
func CleanStepDesc(desc string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return ""
	}
	for _, line := range strings.Split(desc, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
