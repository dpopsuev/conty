package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dpopsuev/conty/internal/port/driver"
	"github.com/dpopsuev/battery/mcpserver"
	"github.com/dpopsuev/battery/server"
	"github.com/dpopsuev/battery/tool"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "conty"
	serverVersion = "0.1.0"
)

var serverInstructions = `Conty — AI-driven CI/CD execution tool. Single point for deploying lab environments and analyzing CI results.

Actions:
  pipeline_trigger  — Trigger a named pipeline (sequential multi-job execution)
  pipeline_status   — Get current status of a pipeline run
  step_log          — Get log output for a specific pipeline step
  pipelines         — List available pipelines
  backends          — List available CI backend names
  ci_check          — Check latest CI run status for a job
  ci_verdict        — Get structured pass/fail verdict with failure context
  ci_redeploy       — Trigger redeployment of a CI job
  ci_trigger        — Trigger a build with custom parameters, returns queue_id and build_number
  ci_params         — Get parameters from a specific build number (clone-and-override workflow)
  ci_history        — List recent builds for a job with status
  ci_log            — Get console output for a specific build number
  ci_poll           — Resolve a queue ID to a build number`

type ContyService interface {
	driver.PipelineService
	driver.CIMonitorService
}

func Serve(svc ContyService) error {
	srv := mcpserver.NewServer(serverName, serverVersion).
		WithInstructions(serverInstructions)
	RegisterTools(srv, svc)
	return srv.Serve(context.Background(), &sdkmcp.StdioTransport{})
}

func RegisterTools(srv *mcpserver.Server, svc ContyService) {
	srv.ToolWithSchema(
		server.ToolMeta{
			Name:        "conty",
			Description: "CI/CD execution — deploy lab environments and analyze CI results",
			Keywords:    []string{"ci", "cd", "pipeline", "deploy", "jenkins", "build"},
			Categories:  []string{"ci-cd"},
		},
		contySchema,
		contyHandler(svc),
	)
}

var contySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"action":  {"type": "string", "enum": ["pipeline_trigger","pipeline_status","step_log","pipelines","backends","ci_check","ci_verdict","ci_redeploy","ci_trigger","ci_params","ci_history","ci_log","ci_poll"], "description": "Action to perform"},
		"name":    {"type": "string", "description": "Pipeline name (pipeline_trigger, pipeline_status, step_log)"},
		"step":    {"type": "integer", "description": "Step index for step_log (0-based)"},
		"backend": {"type": "string", "description": "Backend name (ci_check, ci_verdict, ci_redeploy)"},
		"job_ref": {"type": "string", "description": "Job reference path (ci_check, ci_verdict, ci_redeploy). Use plain job name e.g. 'ocp-baremetal-ipi-deployment', or folder/name e.g. 'CI/far-edge-vran-deployment'"},
		"params":  {"type": "object", "description": "Build parameters as key-value pairs (ci_trigger, ci_redeploy). Example: {\"OPENSHIFT_RELEASE_IMAGE\": \"quay.io/ocp/release:4.22-nightly\"}"},
		"run_id":  {"type": "string", "description": "Build/run number (ci_params, ci_log)"},
		"queue_id": {"type": "string", "description": "Queue item ID from ci_trigger/ci_redeploy (ci_poll)"},
		"limit":   {"type": "integer", "description": "Max results (ci_history, default 10)"}
	},
	"required": ["action"]
}`)

var (
	errUnknownAction   = errors.New("unknown action")
	errNameRequired    = errors.New("name parameter is required")
	errBackendRequired = errors.New("backend parameter is required")
	errJobRefRequired  = errors.New("job_ref parameter is required")
)

type contyArgs struct {
	Action  string            `json:"action"`
	Name    string            `json:"name"`
	Step    int               `json:"step"`
	Backend string            `json:"backend"`
	JobRef  string            `json:"job_ref"`
	Params  map[string]string `json:"params"`
	RunID   string            `json:"run_id"`
	QueueID string            `json:"queue_id"`
	Limit   int               `json:"limit"`
}

func contyHandler(svc ContyService) server.Handler {
	return func(ctx context.Context, input json.RawMessage) (tool.Result, error) {
		var args contyArgs
		if err := json.Unmarshal(input, &args); err != nil {
			return tool.Result{}, fmt.Errorf("invalid arguments: %w", err)
		}

		switch args.Action {
		case "pipeline_trigger":
			if args.Name == "" {
				return tool.Result{}, errNameRequired
			}
			run, err := svc.TriggerPipeline(ctx, args.Name)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(run)

		case "pipeline_status":
			if args.Name == "" {
				return tool.Result{}, errNameRequired
			}
			run, err := svc.GetPipelineStatus(ctx, args.Name)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(run)

		case "step_log":
			if args.Name == "" {
				return tool.Result{}, errNameRequired
			}
			log, err := svc.GetStepLog(ctx, args.Name, args.Step)
			if err != nil {
				return tool.Result{}, err
			}
			return tool.TextResult(log), nil

		case "pipelines":
			return server.JSONResult(map[string]any{
				"pipelines": svc.ListPipelines(),
			})

		case "backends":
			return server.JSONResult(map[string]any{
				"backends": svc.ListBackends(),
			})

		case "ci_check":
			if args.Backend == "" {
				return tool.Result{}, errBackendRequired
			}
			if args.JobRef == "" {
				return tool.Result{}, errJobRefRequired
			}
			check, err := svc.CheckLatest(ctx, args.Backend, args.JobRef)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(check)

		case "ci_verdict":
			if args.Backend == "" {
				return tool.Result{}, errBackendRequired
			}
			if args.JobRef == "" {
				return tool.Result{}, errJobRefRequired
			}
			verdict, err := svc.GetVerdict(ctx, args.Backend, args.JobRef)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(verdict)

		case "ci_redeploy":
			if args.Backend == "" {
				return tool.Result{}, errBackendRequired
			}
			if args.JobRef == "" {
				return tool.Result{}, errJobRefRequired
			}
			runID, err := svc.TriggerRedeployWithParams(ctx, args.Backend, args.JobRef, args.Params)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(map[string]string{"run_id": runID})

		case "ci_trigger":
			if args.Backend == "" {
				return tool.Result{}, errBackendRequired
			}
			if args.JobRef == "" {
				return tool.Result{}, errJobRefRequired
			}
			result, err := svc.CITrigger(ctx, args.Backend, args.JobRef, args.Params)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(result)

		case "ci_params":
			if args.Backend == "" {
				return tool.Result{}, errBackendRequired
			}
			if args.JobRef == "" {
				return tool.Result{}, errJobRefRequired
			}
			if args.RunID == "" {
				return tool.Result{}, fmt.Errorf("run_id parameter is required")
			}
			params, err := svc.CIParams(ctx, args.Backend, args.JobRef, args.RunID)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(params)

		case "ci_history":
			if args.Backend == "" {
				return tool.Result{}, errBackendRequired
			}
			if args.JobRef == "" {
				return tool.Result{}, errJobRefRequired
			}
			builds, err := svc.CIHistory(ctx, args.Backend, args.JobRef, args.Limit)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(builds)

		case "ci_log":
			if args.Backend == "" {
				return tool.Result{}, errBackendRequired
			}
			if args.JobRef == "" {
				return tool.Result{}, errJobRefRequired
			}
			if args.RunID == "" {
				return tool.Result{}, fmt.Errorf("run_id parameter is required")
			}
			log, err := svc.CILog(ctx, args.Backend, args.JobRef, args.RunID)
			if err != nil {
				return tool.Result{}, err
			}
			return tool.TextResult(log), nil

		case "ci_poll":
			if args.Backend == "" {
				return tool.Result{}, errBackendRequired
			}
			if args.QueueID == "" {
				return tool.Result{}, fmt.Errorf("queue_id parameter is required")
			}
			buildNum, err := svc.CIPoll(ctx, args.Backend, args.QueueID)
			if err != nil {
				return tool.Result{}, err
			}
			return server.JSONResult(map[string]string{"build_number": buildNum})

		default:
			return tool.Result{}, fmt.Errorf("%w: %s", errUnknownAction, args.Action)
		}
	}
}
