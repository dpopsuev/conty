// Package scribe translates Conty CI/CD data into Battery canonical Records.
package scribe

import (
	"fmt"
	"strings"

	"github.com/dpopsuev/battery/translate"
	"github.com/dpopsuev/conty/internal/domain"
)

// TranslateBuilds converts CI runs into canonical Records.
func TranslateBuilds(runs []domain.CIRun, backend string) translate.Result {
	var result translate.Result

	for _, run := range runs {
		r := translate.Record{
			ID:    buildID(backend, run.Name, run.ID),
			Kind:  "support.ref",
			Title: fmt.Sprintf("%s #%s", run.Name, run.ID),
			Labels: []string{
				"source:conty",
				"backend:" + backend,
				"result:" + strings.ToLower(string(run.Result)),
			},
			Sections: []translate.Section{
				{Name: "url", Text: run.URL},
			},
			Extra: map[string]any{
				"status":   string(run.Status),
				"result":   string(run.Result),
				"duration": run.Duration,
			},
		}
		if !run.StartedAt.IsZero() {
			r.Extra["started_at"] = run.StartedAt.Format("2006-01-02T15:04:05Z")
		}
		result.Records = append(result.Records, r)

		if run.UpstreamJob != "" && run.UpstreamRunID != "" {
			result.Edges = append(result.Edges, translate.Edge{
				From:     buildID(backend, run.UpstreamJob, run.UpstreamRunID),
				Relation: "produced_by",
				To:       r.ID,
			})
		}
	}

	return result
}

// TranslateBuildTree recursively converts a build chain into Records + parent_of edges.
func TranslateBuildTree(node *domain.CIRunNode, backend string) translate.Result {
	var result translate.Result
	walkTree(node, backend, &result)
	return result
}

func walkTree(node *domain.CIRunNode, backend string, result *translate.Result) {
	id := buildID(backend, node.JobRef, node.RunID)
	r := translate.Record{
		ID:    id,
		Kind:  "support.ref",
		Title: fmt.Sprintf("%s #%s", node.Name, node.RunID),
		Labels: []string{
			"source:conty",
			"backend:" + backend,
			"result:" + strings.ToLower(string(node.Result)),
		},
		Extra: map[string]any{
			"status":   string(node.Status),
			"result":   string(node.Result),
			"duration": node.Duration,
		},
	}
	result.Records = append(result.Records, r)

	for i := range node.Children {
		child := &node.Children[i]
		childID := buildID(backend, child.JobRef, child.RunID)
		result.Edges = append(result.Edges, translate.Edge{
			From:     id,
			Relation: "parent_of",
			To:       childID,
		})
		walkTree(child, backend, result)
	}
}

func buildID(backend, job, runID string) string {
	return fmt.Sprintf("%s:%s/%s", backend, job, runID)
}
