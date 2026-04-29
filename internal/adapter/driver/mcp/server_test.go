package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	contymcp "github.com/dpopsuev/conty/internal/adapter/driver/mcp"
	"github.com/dpopsuev/conty/internal/app"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driven/driventest"
	"github.com/dpopsuev/battery/mcpserver"
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

	srv := mcpserver.NewServer("test", "0.0.1")
	contymcp.RegisterTools(srv, svc)

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() {
		_ = srv.Serve(ctx, serverTransport)
	}()

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

func TestAllListActions_ReturnJSONObjects(t *testing.T) {
	session := setupServer(t)

	actions := []struct {
		name string
		args map[string]any
	}{
		{"pipelines", map[string]any{"action": "pipelines"}},
		{"backends", map[string]any{"action": "backends"}},
		{"backend_info", map[string]any{"action": "backend_info"}},
		{"ci_owned", map[string]any{"action": "ci_owned"}},
		{"ci_history", map[string]any{"action": "ci_history", "backend": "test", "job_ref": "job-a", "limit": 5}},
		{"ci_artifacts", map[string]any{"action": "ci_artifacts", "backend": "test", "job_ref": "job-a", "run_id": "run-1"}},
	}

	for _, tt := range actions {
		t.Run(tt.name, func(t *testing.T) {
			result := callTool(t, session, tt.args)
			if len(result.Content) == 0 {
				t.Fatal("empty content")
			}
			content := result.Content[0].(*sdkmcp.TextContent)
			text := content.Text
			if len(text) == 0 {
				t.Fatal("empty text")
			}
			if text[0] == '[' {
				t.Errorf("action %s returns bare JSON array — must wrap in object", tt.name)
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(text), &obj); err != nil {
				t.Errorf("action %s response is not a JSON object: %v\nraw: %s", tt.name, err, text)
			}
		})
	}
}
