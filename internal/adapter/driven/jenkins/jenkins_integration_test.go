//go:build integration

package jenkins_test

import (
	"context"
	"os"
	"testing"

	"github.com/DanyPops/conty/internal/adapter/driven/jenkins"
	"github.com/DanyPops/conty/internal/port/driven"
	"github.com/DanyPops/conty/internal/port/driven/driventest"
)

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
