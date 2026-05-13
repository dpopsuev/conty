package app

import (
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
)

func TestClassifyLog(t *testing.T) {
	tests := []struct {
		name     string
		log      string
		wantClass domain.FailureClassification
		wantRetry bool
	}{
		{
			name:      "connection timeout",
			log:       "error: connection timed out after 30s",
			wantClass: domain.FailureNetwork,
			wantRetry: true,
		},
		{
			name:      "deadline exceeded",
			log:       "context deadline exceeded while waiting for pod",
			wantClass: domain.FailureNetwork,
			wantRetry: true,
		},
		{
			name:      "OOMKilled",
			log:       "Container OOMKilled — exit code 137",
			wantClass: domain.FailureInfra,
			wantRetry: true,
		},
		{
			name:      "no space left",
			log:       "cp: error writing '/tmp/image.iso': No space left on device",
			wantClass: domain.FailureInfra,
			wantRetry: true,
		},
		{
			name:      "imagepullbackoff",
			log:       "Warning  BackOff  ImagePullBackOff",
			wantClass: domain.FailureInfra,
			wantRetry: true,
		},
		{
			name:      "auth failure",
			log:       "Error: authentication failed: 401 Unauthorized",
			wantClass: domain.FailureConfig,
			wantRetry: false,
		},
		{
			name:      "permission denied",
			log:       "open /etc/kubernetes/admin.conf: permission denied",
			wantClass: domain.FailureConfig,
			wantRetry: false,
		},
		{
			name:      "test failure",
			log:       "FAILED: TestInstallCluster — assertion failed: expected Ready, got NotReady",
			wantClass: domain.FailureTest,
			wantRetry: false,
		},
		{
			name:      "unknown",
			log:       "something went wrong but no pattern matched",
			wantClass: domain.FailureUnknown,
			wantRetry: false,
		},
		{
			name:      "empty log",
			log:       "",
			wantClass: domain.FailureUnknown,
			wantRetry: false,
		},
		{
			name:      "network beats test — first match wins",
			log:       "connection timed out\nFAILED: test assertions",
			wantClass: domain.FailureNetwork,
			wantRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClass, gotRetry := classifyLog(tt.log)
			if gotClass != tt.wantClass {
				t.Errorf("class = %q, want %q", gotClass, tt.wantClass)
			}
			if gotRetry != tt.wantRetry {
				t.Errorf("canRetry = %v, want %v", gotRetry, tt.wantRetry)
			}
		})
	}
}
