package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contymcp "github.com/dpopsuev/conty/internal/adapter/driver/mcp"
	"github.com/dpopsuev/conty/internal/app"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven/driventest"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func setupServer(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()

	stub := driventest.NewStubCIAdapter("test")
	stub.RunID = "run-1"
	stub.QueueID = "build-1"
	stub.Run = &domain.CIRun{
		ID:     "run-1",
		Status: domain.RunStatusSuccess,
		Result: domain.RunResultSuccess,
	}
	stub.Builds = []domain.CIRun{
		{ID: "1", Status: domain.RunStatusSuccess},
	}
	stub.Artifacts = []domain.CIArtifact{
		{Name: "report.xml", Path: "report.xml"},
	}

	svc := app.NewService(stub)
	svc.RegisterPipeline(domain.Pipeline{
		Name: "test-pipe", Backend: "test",
		Steps: []domain.PipelineStep{{JobName: "step-1"}},
	})

	srv := contymcp.NewBatteryServer(svc)

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = srv.Serve(ctx, serverTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, session *sdkmcp.ClientSession, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "conty",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%v): %v", args["action"], err)
	}
	return result
}

func getText(t *testing.T, result *sdkmcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("empty content")
	}
	return result.Content[0].(*sdkmcp.TextContent).Text
}

func TestAllReadActions_ReturnContent(t *testing.T) {
	session := setupServer(t)

	actions := []struct {
		name string
		args map[string]any
	}{
		{"help", map[string]any{"action": "help"}},
		{"search-owned", map[string]any{"action": "search", "owned": true}},
		{"search-builds", map[string]any{"action": "search", "backend": "test", "job_ref": "job-a"}},
		{"artifact-list", map[string]any{"action": "artifact", "backend": "test", "job_ref": "job-a", "run_id": "run-1"}},
		{"status-pipeline", map[string]any{"action": "status", "pipeline": "test-pipe"}},
	}

	for _, tt := range actions {
		t.Run(tt.name, func(t *testing.T) {
			result := callTool(t, session, tt.args)
			text := getText(t, result)
			if len(text) == 0 {
				t.Fatal("empty text")
			}
			// JSON results must not be bare arrays
			if text[0] == '[' {
				t.Errorf("action %s returns bare JSON array — must wrap in object or be text", tt.name)
			}
		})
	}
}

func TestHelp_ListsBackendsAndActions(t *testing.T) {
	session := setupServer(t)
	result := callTool(t, session, map[string]any{"action": "help"})
	text := getText(t, result)

	for _, want := range []string{"status", "log", "search", "trigger", "wait", "artifact", "cancel"} {
		if !strings.Contains(text, want) {
			t.Errorf("help output missing action %q", want)
		}
	}
}

// TestServeHTTP_StatelessSurvivesRestart verifies that the HTTP server can be
// called without a prior session — simulating what happens when the server
// restarts and the client's cached Mcp-Session-Id is stale or absent.
func TestServeHTTP_StatelessSurvivesRestart(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Run = &domain.CIRun{ID: "run-1", Status: domain.RunStatusSuccess, Result: domain.RunResultSuccess}

	svc := app.NewService(stub)
	ts := httptest.NewServer(contymcp.NewHTTPHandler(svc))
	t.Cleanup(ts.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)

	call := func(t *testing.T, label string) {
		t.Helper()
		conn, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
		if err != nil {
			t.Fatalf("%s connect: %v", label, err)
		}
		defer func() { _ = conn.Close() }()
		result, err := conn.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      "conty",
			Arguments: map[string]any{"action": "help"},
		})
		if err != nil {
			t.Fatalf("%s call: %v", label, err)
		}
		if len(result.Content) == 0 {
			t.Fatalf("%s: empty content", label)
		}
	}

	call(t, "first")
	call(t, "second") // simulates post-restart with fresh connection
}

// TestServeHTTP_StaleSessionIDIgnored verifies stateless mode ignores bad session IDs.
func TestServeHTTP_StaleSessionIDIgnored(t *testing.T) {
	stub := driventest.NewStubCIAdapter("test")
	stub.Run = &domain.CIRun{ID: "run-1", Status: domain.RunStatusSuccess}
	svc := app.NewService(stub)

	ts := httptest.NewServer(contymcp.NewHTTPHandler(svc))
	t.Cleanup(ts.Close)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", "stale-session-id-that-does-not-exist")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("got 404 with stale session ID — stateless mode broken")
	}
}

func TestSearch_OwnedReturnsList(t *testing.T) {
	session := setupServer(t)
	result := callTool(t, session, map[string]any{"action": "search", "owned": true})
	text := getText(t, result)
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	if _, ok := obj["builds"]; !ok {
		t.Errorf("missing builds key: %s", text)
	}
}
