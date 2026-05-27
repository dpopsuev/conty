//go:build integration

package jenkins_test

import (
	"context"
	"os"
	"testing"

	"github.com/dpopsuev/conty/internal/adapter/driven/jenkins"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven"
	"github.com/dpopsuev/conty/internal/port/driven/driventest"
)

// knownAnchor is the historical build pair used as integration test anchors.
// CI/far-edge-vran-deployment #17913 triggered ocp-far-edge-vran-tests #6527.
const (
	anchorUpstreamJob   = "CI/far-edge-vran-deployment"
	anchorUpstreamRunID = "17913"
	anchorDownstreamJob = "ocp-far-edge-vran-tests"
	anchorDownstreamID  = "6527"
)

func jenkinsCI(t *testing.T) *jenkins.Adapter {
	t.Helper()
	url := os.Getenv("JENKINS_URL")
	user := os.Getenv("JENKINS_USER")
	token := os.Getenv("JENKINS_CI_API_KEY")
	if url == "" || user == "" || token == "" {
		t.Skip("JENKINS_URL, JENKINS_USER, JENKINS_CI_API_KEY required")
	}
	a, err := jenkins.New(context.Background(), "jenkins-ci", url, user, token)
	if err != nil {
		t.Fatalf("jenkins.New: %v", err)
	}
	return a
}

func TestSearchBuildsUpstreamCause(t *testing.T) {
	a := jenkinsCI(t)

	builds, err := a.SearchBuilds(context.Background(), anchorDownstreamJob, domain.BuildFilter{Limit: 20})
	if err != nil {
		t.Fatalf("SearchBuilds: %v", err)
	}
	if len(builds) == 0 {
		t.Skip("no builds available in job")
	}

	// At least one recent build should have a non-empty upstream job.
	var found bool
	for _, b := range builds {
		if b.UpstreamJob != "" && b.UpstreamRunID != "" {
			found = true
			t.Logf("found upstream cause: %s #%s -> %s #%s",
				anchorDownstreamJob, b.ID, b.UpstreamJob, b.UpstreamRunID)
			break
		}
	}
	if !found {
		t.Log("no upstream cause found in recent builds — may be user-triggered only")
	}
}

func TestGetDownstreamRuns_Live(t *testing.T) {
	a := jenkinsCI(t)

	runs, err := a.GetDownstreamRuns(context.Background(),
		anchorDownstreamJob, anchorUpstreamJob, anchorUpstreamRunID)
	if err != nil {
		t.Fatalf("GetDownstreamRuns: %v", err)
	}

	t.Logf("downstream runs found: %d", len(runs))
	for _, r := range runs {
		t.Logf("  run %s: status=%s upstream=%s#%s", r.ID, r.Status, r.UpstreamJob, r.UpstreamRunID)
	}

	if len(runs) == 0 {
		t.Logf("no downstream runs found for %s#%s — anchor build may be outside the 50-build window",
			anchorUpstreamJob, anchorUpstreamRunID)
		return
	}

	// Assert the known anchor downstream build is present.
	var found bool
	for _, r := range runs {
		if r.ID == anchorDownstreamID {
			found = true
			if r.UpstreamJob != anchorUpstreamJob {
				t.Errorf("UpstreamJob = %q, want %q", r.UpstreamJob, anchorUpstreamJob)
			}
			if r.UpstreamRunID != anchorUpstreamRunID {
				t.Errorf("UpstreamRunID = %q, want %q", r.UpstreamRunID, anchorUpstreamRunID)
			}
			break
		}
	}
	if !found {
		t.Logf("anchor run %s not in window — older than 50 recent builds", anchorDownstreamID)
	}
}

func TestJenkinsAdapter_ContractCompliance(t *testing.T) {
	url := os.Getenv("JENKINS_URL")
	user := os.Getenv("JENKINS_USER")
	token := os.Getenv("JENKINS_AUTO_API_KEY")
	if url == "" || user == "" || token == "" {
		t.Skip("JENKINS_URL, JENKINS_USER, JENKINS_AUTO_API_KEY required")
	}

	jobName := os.Getenv("JENKINS_TEST_JOB")
	if jobName == "" {
		jobName = "ocp-baremetal-ipi-deployment"
	}

	adapter, err := jenkins.New(context.Background(), "jenkins-auto", url, user, token)
	if err != nil {
		t.Fatalf("jenkins.New: %v", err)
	}

	run, err := adapter.PollRun(context.Background(), jobName, "latest")
	if err != nil {
		t.Fatalf("resolve latest build: %v", err)
	}
	resolvedID := run.ID

	driventest.RunCIAdapterContractTests(t, func(t *testing.T) (driven.CIAdapter, string, string) {
		return adapter, jobName, resolvedID
	})
}
