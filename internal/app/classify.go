package app

import (
	"strings"

	"github.com/dpopsuev/conty/internal/domain"
)

// classifyLog inspects a build log excerpt and returns the best-fit
// FailureClassification and whether the failure is safe to retry.
//
// Patterns are ordered from most-specific to least-specific.
// The first match wins.
func classifyLog(log string) (domain.FailureClassification, bool) {
	lower := strings.ToLower(log)

	for _, rule := range classificationRules {
		for _, pat := range rule.patterns {
			if strings.Contains(lower, pat) {
				return rule.class, rule.canRetry
			}
		}
	}
	return domain.FailureUnknown, false
}

type classificationRule struct {
	class    domain.FailureClassification
	canRetry bool
	patterns []string
}

// classificationRules is evaluated top-to-bottom; first match wins.
var classificationRules = []classificationRule{
	{
		class:    domain.FailureNetwork,
		canRetry: true,
		patterns: []string{
			"connection timed out",
			"connection refused",
			"connection reset by peer",
			"i/o timeout",
			"read timeout",
			"write timeout",
			"deadline exceeded",
			"network unreachable",
			"no route to host",
			"temporary failure in name resolution",
			"dial tcp",
			"tls handshake timeout",
			"econnreset",
			"econnrefused",
			"etimedout",
		},
	},
	{
		class:    domain.FailureInfra,
		canRetry: true,
		patterns: []string{
			"no space left on device",
			"out of memory",
			"oomkilled",
			"cannot allocate memory",
			"killed: 9",
			"signal: killed",
			"node is not available",
			"node not found",
			"evicted",
			"crashloopbackoff",
			"imagepullbackoff",
			"failed to pull image",
			"disk pressure",
			"memory pressure",
			"pod has been evicted",
		},
	},
	{
		class:    domain.FailureConfig,
		canRetry: false,
		patterns: []string{
			"invalid configuration",
			"configuration error",
			"config not found",
			"missing required",
			"undefined variable",
			"syntax error",
			"parse error",
			"invalid value",
			"unknown option",
			"permission denied",
			"access denied",
			"unauthorized",
			"authentication failed",
			"certificate verify failed",
		},
	},
	{
		class:    domain.FailureTest,
		canRetry: false,
		patterns: []string{
			"tests failed",
			"test failed",
			"assertion failed",
			"assertionerror",
			"expected but was",
			"failures:",
			"failed tests:",
			"build failed",
			"[fail]",
			"--- fail",
		},
	},
}
