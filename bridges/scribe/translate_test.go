package scribe_test

import (
	"testing"

	bridge "github.com/dpopsuev/conty/bridges/scribe"
	"github.com/dpopsuev/conty/testdata"
)

const testBackend = "jenkins"

func TestTranslateBuilds_Records(t *testing.T) {
	builds := testdata.SampleBuilds()
	result := bridge.TranslateBuilds(builds, testBackend)

	if len(result.Records) != 3 {
		t.Fatalf("records = %d; want 3", len(result.Records))
	}

	success := result.Records[0]
	if success.Kind != "support.ref" {
		t.Errorf("kind = %q; want support.ref", success.Kind)
	}
	if success.Title != "deploy-staging #42" {
		t.Errorf("title = %q; want 'deploy-staging #42'", success.Title)
	}

	hasSource := false
	for _, l := range success.Labels {
		if l == "source:conty" {
			hasSource = true
		}
	}
	if !hasSource {
		t.Error("missing source:conty label")
	}
}

func TestTranslateBuilds_UpstreamEdge(t *testing.T) {
	builds := testdata.SampleBuilds()
	result := bridge.TranslateBuilds(builds, testBackend)

	if len(result.Edges) != 1 {
		t.Fatalf("edges = %d; want 1 (e2e-tests upstream)", len(result.Edges))
	}

	edge := result.Edges[0]
	if edge.Relation != "produced_by" {
		t.Errorf("relation = %q; want produced_by", edge.Relation)
	}
	if edge.From != "jenkins:deploy-staging/42" {
		t.Errorf("from = %q; want jenkins:deploy-staging/42", edge.From)
	}
}

func TestTranslateBuildTree(t *testing.T) {
	tree := testdata.SampleBuildTree()
	result := bridge.TranslateBuildTree(tree, testBackend)

	if len(result.Records) != 3 {
		t.Fatalf("records = %d; want 3 (parent + 2 children)", len(result.Records))
	}
	if len(result.Edges) != 2 {
		t.Fatalf("edges = %d; want 2 parent_of edges", len(result.Edges))
	}

	for _, e := range result.Edges {
		if e.Relation != "parent_of" {
			t.Errorf("edge relation = %q; want parent_of", e.Relation)
		}
	}
}

func TestTranslateBuilds_Empty(t *testing.T) {
	result := bridge.TranslateBuilds(nil, testBackend)
	if len(result.Records) != 0 {
		t.Errorf("records = %d; want 0", len(result.Records))
	}
}
