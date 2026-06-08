package jenkins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dpopsuev/conty/internal/domain"
)

// buildPayload constructs a minimal Jenkins build JSON entry with a single upstream cause.
func buildPayload(number int, result, upstreamProject string, upstreamBuild int) map[string]any {
	return map[string]any{
		"number":          number,
		"result":          result,
		"fullDisplayName": "test-job #" + strings.ReplaceAll(result, " ", ""),
		"timestamp":       int64(1700000000000),
		"duration":        int64(3600000),
		"url":             "http://jenkins/job/test/",
		"building":        false,
		"actions": []any{
			map[string]any{
				"causes": []any{
					map[string]any{
						"upstreamProject": upstreamProject,
						"upstreamBuild":   upstreamBuild,
					},
				},
			},
		},
	}
}

func TestGetDownstreamRuns(t *testing.T) {
	var capturedURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"builds": []any{
				// Build A: exact cause match — should be returned.
				buildPayload(6527, "SUCCESS", "CI/far-edge-vran-deployment", 17913),
				// Build B: different upstreamProject — filtered out.
				buildPayload(6520, "SUCCESS", "CI/other-job", 17913),
				// Build C: matching project but upstreamBuild=0 — filtered out.
				buildPayload(6510, "FAILURE", "CI/far-edge-vran-deployment", 0),
				// Build D: matching project, different build number — filtered out.
				buildPayload(6500, "SUCCESS", "CI/far-edge-vran-deployment", 17000),
			},
		})
	}))
	defer srv.Close()

	a := &Adapter{name: "test", baseURL: srv.URL, user: "u", token: "t"}
	runs, err := a.GetDownstreamRuns(context.Background(), "ocp-far-edge-vran-tests", "CI/far-edge-vran-deployment", "17913")
	if err != nil {
		t.Fatalf("GetDownstreamRuns: %v", err)
	}

	// Verify URL construction.
	if !strings.Contains(capturedURL, "/job/ocp-far-edge-vran-tests/api/json") {
		t.Errorf("unexpected job path in URL: %s", capturedURL)
	}
	if !strings.Contains(capturedURL, "upstreamBuild") {
		t.Errorf("tree param missing upstreamBuild: %s", capturedURL)
	}

	// Verify filter: only build A passes.
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d: %+v", len(runs), runs)
	}
	if runs[0].ID != "6527" {
		t.Errorf("expected run ID '6527', got %q", runs[0].ID)
	}
	if runs[0].UpstreamJob != "CI/far-edge-vran-deployment" {
		t.Errorf("UpstreamJob = %q, want 'CI/far-edge-vran-deployment'", runs[0].UpstreamJob)
	}
	if runs[0].UpstreamRunID != "17913" {
		t.Errorf("UpstreamRunID = %q, want '17913'", runs[0].UpstreamRunID)
	}
}

func TestGetDownstreamRuns_FolderPath(t *testing.T) {
	var capturedURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"builds": []any{}})
	}))
	defer srv.Close()

	a := &Adapter{name: "test", baseURL: srv.URL, user: "u", token: "t"}
	_, err := a.GetDownstreamRuns(context.Background(), "CI/ocp-far-edge-vran-tests", "CI/far-edge-vran-deployment", "17913")
	if err != nil {
		t.Fatalf("GetDownstreamRuns with folder path: %v", err)
	}
	// Folder path CI/ocp-far-edge-vran-tests must expand to /job/CI/job/ocp-far-edge-vran-tests.
	if !strings.Contains(capturedURL, "/job/CI/job/ocp-far-edge-vran-tests/api/json") {
		t.Errorf("folder path not expanded correctly in URL: %s", capturedURL)
	}
}

func TestGetDownstreamRuns_CaseInsensitiveMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return cause with uppercase project name.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"builds": []any{
				buildPayload(100, "SUCCESS", "ci/far-edge-vran-deployment", 42),
			},
		})
	}))
	defer srv.Close()

	a := &Adapter{name: "test", baseURL: srv.URL, user: "u", token: "t"}
	// Query with original casing — should still match (EqualFold).
	runs, err := a.GetDownstreamRuns(context.Background(), "job", "CI/far-edge-vran-deployment", "42")
	if err != nil {
		t.Fatalf("GetDownstreamRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("expected 1 case-insensitive match, got %d", len(runs))
	}
}

func TestGetDownstreamRuns_InvalidRunID(t *testing.T) {
	a := &Adapter{name: "test", baseURL: "http://localhost", user: "u", token: "t"}
	_, err := a.GetDownstreamRuns(context.Background(), "job", "upstream", "not-a-number")
	if err == nil {
		t.Fatal("expected error for non-numeric upstreamRunID")
	}
	if !strings.Contains(err.Error(), "invalid upstream_run_id") {
		t.Errorf("unexpected error text: %v", err)
	}
}

func TestGetDownstreamRuns_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	a := &Adapter{name: "test", baseURL: srv.URL, user: "u", token: "t"}
	_, err := a.GetDownstreamRuns(context.Background(), "missing-job", "upstream", "123")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestMapCauses_Empty(t *testing.T) {
	uj, uri := mapCauses(nil)
	if uj != "" || uri != "" {
		t.Errorf("expected empty results for nil actions, got %q %q", uj, uri)
	}
}

func TestMapCauses_FirstUpstreamWins(t *testing.T) {
	actions := []buildAction{
		{Causes: []struct {
			UserID          string `json:"userId"`
			UserName        string `json:"userName"`
			UpstreamBuild   int64  `json:"upstreamBuild"`
			UpstreamProject string `json:"upstreamProject"`
		}{
			{UpstreamProject: "job-a", UpstreamBuild: 100},
			{UpstreamProject: "job-b", UpstreamBuild: 200},
		}},
	}
	uj, uri := mapCauses(actions)
	if uj != "job-a" || uri != "100" {
		t.Errorf("expected first cause to win: got %q %q", uj, uri)
	}
}

// --- parseChildrenFromDescription ---

func TestParseChildrenFromDescription_Empty(t *testing.T) {
	if got := parseChildrenFromDescription(""); got != nil {
		t.Errorf("expected nil for empty description, got %v", got)
	}
}

func TestParseChildrenFromDescription_NoLinks(t *testing.T) {
	if got := parseChildrenFromDescription("Started by upstream project CI/foo build number 42"); got != nil {
		t.Errorf("expected nil when no job links present, got %v", got)
	}
}

func TestParseChildrenFromDescription_SingleChildRelative(t *testing.T) {
	desc := `<a href='/job/CI/job/ocp-far-edge-vran-tests/6665/'>ocp-far-edge-vran-tests #6665</a>`
	got := parseChildrenFromDescription(desc)
	if len(got) != 1 {
		t.Fatalf("expected 1 child, got %d: %v", len(got), got)
	}
	if got[0].JobRef != "CI/ocp-far-edge-vran-tests" {
		t.Errorf("JobRef = %q, want %q", got[0].JobRef, "CI/ocp-far-edge-vran-tests")
	}
	if got[0].RunID != "6665" {
		t.Errorf("RunID = %q, want %q", got[0].RunID, "6665")
	}
}

func TestParseChildrenFromDescription_SingleChildAbsoluteURL(t *testing.T) {
	desc := `<a href="https://jenkins-csb-kniqe-ci.dno.corp.redhat.com/job/CI/job/ocp-far-edge-vran-tests/6665/">link</a>`
	got := parseChildrenFromDescription(desc)
	if len(got) != 1 {
		t.Fatalf("expected 1 child, got %d: %v", len(got), got)
	}
	if got[0].JobRef != "CI/ocp-far-edge-vran-tests" {
		t.Errorf("JobRef = %q, want %q", got[0].JobRef, "CI/ocp-far-edge-vran-tests")
	}
	if got[0].RunID != "6665" {
		t.Errorf("RunID = %q, want %q", got[0].RunID, "6665")
	}
}

func TestParseChildrenFromDescription_MultipleChildren(t *testing.T) {
	desc := `<a href='/job/CI/job/ocp-far-edge-vran-tests/6665/'>tests</a>, ` +
		`<a href='/job/CI/job/ocp-far-edge-vran-collect/1234/'>collect</a>`
	got := parseChildrenFromDescription(desc)
	if len(got) != 2 {
		t.Fatalf("expected 2 children, got %d: %v", len(got), got)
	}
}

func TestParseChildrenFromDescription_Deduplication(t *testing.T) {
	desc := `<a href='/job/CI/job/ocp-far-edge-vran-tests/6665/'>a</a> ` +
		`<a href='/job/CI/job/ocp-far-edge-vran-tests/6665/'>b</a>`
	got := parseChildrenFromDescription(desc)
	if len(got) != 1 {
		t.Errorf("expected duplicate links to be deduplicated, got %d: %v", len(got), got)
	}
}

func TestParseChildrenFromDescription_TopLevelJob(t *testing.T) {
	desc := `<a href='/job/ocp-far-edge-vran-tests/6665/'>tests</a>`
	got := parseChildrenFromDescription(desc)
	if len(got) != 1 {
		t.Fatalf("expected 1 child, got %d", len(got))
	}
	if got[0].JobRef != "ocp-far-edge-vran-tests" {
		t.Errorf("JobRef = %q, want %q", got[0].JobRef, "ocp-far-edge-vran-tests")
	}
}

func TestParseChildrenFromDescription_IgnoresNonBuildLinks(t *testing.T) {
	// href points to a job overview page (no build number) — must be ignored.
	desc := `<a href='/job/CI/job/ocp-far-edge-vran-tests/'>job index</a>`
	got := parseChildrenFromDescription(desc)
	if len(got) != 0 {
		t.Errorf("expected 0 children for job-index link, got %d: %v", len(got), got)
	}
}

// --- SearchRuns ---

func searchRunsPayload(builds []map[string]any) map[string]any {
	return map[string]any{"builds": builds}
}

func buildSearchEntry(number int, result string, tsMs int64, description string) map[string]any {
	return map[string]any{
		"number":          number,
		"result":          result,
		"fullDisplayName": "job #" + strconv.Itoa(number),
		"timestamp":       tsMs,
		"duration":        int64(60000),
		"url":             "http://jenkins/job/test/" + strconv.Itoa(number) + "/",
		"building":        false,
		"description":     description,
		"culprits":        []any{},
		"actions":         []any{},
	}
}

func TestSearchRuns_SinceBreaksEarly(t *testing.T) {
	// Build 300 is at T+300s (newest), build 100 is at T+100s, build 50 is at T+50s.
	// Since = T+200s → only build 300 qualifies.
	// Critically, once we see build 100 (before Since), we must stop iterating —
	// build 50 must never be evaluated.
	base := int64(1700000000000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(searchRunsPayload([]map[string]any{
			buildSearchEntry(300, "SUCCESS", base+300000, ""),
			buildSearchEntry(100, "SUCCESS", base+100000, ""),
			buildSearchEntry(50, "FAILURE", base+50000, ""), // must not appear even if FAILURE matches
		}))
	}))
	defer srv.Close()

	a := &Adapter{name: "test", baseURL: srv.URL, user: "u", token: "t"}
	since := time.UnixMilli(base + 200000)
	runs, err := a.SearchRuns(context.Background(), "my-job", domain.BuildFilter{Since: since})
	if err != nil {
		t.Fatalf("SearchRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run (build 300), got %d: %v", len(runs), runs)
	}
	if runs[0].ID != "300" {
		t.Errorf("expected build 300, got %q", runs[0].ID)
	}
}

func TestSearchRuns_DescriptionChildren(t *testing.T) {
	desc := `<a href='/job/CI/job/ocp-far-edge-vran-tests/6665/'>tests</a>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(searchRunsPayload([]map[string]any{
			buildSearchEntry(42, "SUCCESS", 1700000000000, desc),
		}))
	}))
	defer srv.Close()

	a := &Adapter{name: "test", baseURL: srv.URL, user: "u", token: "t"}
	runs, err := a.SearchRuns(context.Background(), "my-job", domain.BuildFilter{})
	if err != nil {
		t.Fatalf("SearchRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if len(runs[0].Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(runs[0].Children))
	}
	if runs[0].Children[0].JobRef != "CI/ocp-far-edge-vran-tests" {
		t.Errorf("child JobRef = %q, want %q", runs[0].Children[0].JobRef, "CI/ocp-far-edge-vran-tests")
	}
	if runs[0].Children[0].RunID != "6665" {
		t.Errorf("child RunID = %q, want %q", runs[0].Children[0].RunID, "6665")
	}
}

func TestBuildJobPath(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"my-job", "/job/my-job"},
		{"CI/far-edge-vran-deployment", "/job/CI/job/far-edge-vran-deployment"},
		{"a/b/c", "/job/a/job/b/job/c"},
	}
	for _, tt := range cases {
		got := buildJobPath(tt.input)
		if got != tt.want {
			t.Errorf("buildJobPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
