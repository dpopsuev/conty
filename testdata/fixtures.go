// Package testdata provides pre-built CI/CD fixtures for cross-service testing.
package testdata

import (
	"time"

	"github.com/dpopsuev/conty/internal/domain"
)

// SampleBuilds returns a set of CI runs spanning success and failure.
func SampleBuilds() []domain.CIRun {
	return []domain.CIRun{
		{
			ID: "42", Name: "deploy-staging",
			Status: domain.RunStatusSuccess, Result: domain.RunResultSuccess,
			URL: "https://jenkins.example.com/job/deploy-staging/42",
			StartedAt: time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC),
			Duration:  180000,
		},
		{
			ID: "43", Name: "deploy-staging",
			Status: domain.RunStatusFailure, Result: domain.RunResultFailure,
			URL: "https://jenkins.example.com/job/deploy-staging/43",
			StartedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
			Duration:  45000,
		},
		{
			ID: "100", Name: "e2e-tests",
			Status: domain.RunStatusSuccess, Result: domain.RunResultSuccess,
			URL:         "https://jenkins.example.com/job/e2e-tests/100",
			StartedAt:   time.Date(2026, 6, 16, 10, 5, 0, 0, time.UTC),
			Duration:    600000,
			UpstreamJob: "deploy-staging", UpstreamRunID: "42",
		},
	}
}

// SampleBuildTree returns a parent→child build chain.
func SampleBuildTree() *domain.CIRunNode {
	return &domain.CIRunNode{
		JobRef: "deploy-staging", RunID: "42", Name: "deploy-staging",
		Status: domain.RunStatusSuccess, Result: domain.RunResultSuccess,
		Duration: 180000,
		Children: []domain.CIRunNode{
			{
				JobRef: "e2e-tests", RunID: "100", Name: "e2e-tests",
				Status: domain.RunStatusSuccess, Result: domain.RunResultSuccess,
				Duration: 600000,
			},
			{
				JobRef: "smoke-tests", RunID: "55", Name: "smoke-tests",
				Status: domain.RunStatusSuccess, Result: domain.RunResultSuccess,
				Duration: 30000,
			},
		},
	}
}
