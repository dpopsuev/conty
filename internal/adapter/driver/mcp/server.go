package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dpopsuev/battery/mcpserver"
	battserver "github.com/dpopsuev/battery/server"
	"github.com/dpopsuev/battery/tool"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/dpopsuev/conty/internal/port/driver"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverName = "conty"

var Version = "dev"

const serverInstructions = "CI/CD operations. Call ci(action=help) first — lists backends, pipelines, and all actions with params. " +
	"Typical flow: trigger → wait → status. For failures: status(grep=error) gives verdict + filtered log in one call. " +
	"run_id is optional for status/log — omit to use the latest build. " +
	"SEARCH FIRST — before reaching for curl or bash API loops, use search(params={key:value}) to find builds by " +
	"parameter value: search(backend=X, job_ref=Y, params={\"VERSION\":\"5.0\"}) returns all matching builds. " +
	"Combine with result=SUCCESS|FAILURE and since=<RFC3339> for precise filtering. " +
	"Never write a bash loop over Jenkins API when search(params=) covers the use case. " +
	"CHAIN for hierarchy — use chain(backend, job_ref, run_id) to get the full nested build tree in one call " +
	"instead of repeated downstream lookups. Returns CIRunNode with recursive children."

var contySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"action":   {"type": "string", "enum": ["help","status","log","search","trigger","wait","artifact","cancel","upstream","downstream","chain","stages"], "description": "Action to perform. Call help first to see backends and pipelines."},
		"backend":  {"type": "string", "description": "Backend name (listed by help)"},
		"job_ref":  {"type": "string", "description": "Job path e.g. 'ocp-baremetal-ipi-deployment' or 'CI/far-edge-vran-deployment'"},
		"run_id":   {"type": "string", "description": "Build number. Optional for status/log — omit to use the latest build."},
		"queue_id":   {"type": "string", "description": "Queue item ID returned by trigger (deprecated: use opaque_ref). Pass to wait to resolve to build_number."},
		"opaque_ref": {"type": "string", "description": "Opaque trigger reference returned by trigger (replaces queue_id). Pass to wait to resolve to build_number."},
		"pipeline": {"type": "string", "description": "Pipeline name. Use instead of backend+job_ref for pipeline operations (trigger, status, log)."},
		"step":     {"type": "integer", "description": "Step index for pipeline log (0-based). Use with pipeline."},
		"params":   {"type": "object", "description": "Key-value pairs: build parameters for trigger, or parameter filter for search."},
		"result":   {"type": "string", "description": "Filter by result: SUCCESS, FAILURE, ABORTED (search)."},
		"runner":   {"type": "string", "description": "Filter by triggering user userId or userName (search)."},
		"since":    {"type": "string", "description": "RFC 3339 lower bound on build start time (search)."},
		"limit":    {"type": "integer", "description": "Max results (search default 20)."},
		"owned":    {"type": "boolean", "description": "Filter to builds triggered by this session (search)."},
		"path":     {"type": "string", "description": "Artifact path (artifact). Omit to list all artifacts for the build."},
		"tail":     {"type": "integer", "description": "Lines from end of log (default 200, -1 = all). Applies to status, log, artifact."},
		"grep":     {"type": "string", "description": "Return only log lines containing this substring, case-insensitive. Applies to status, log, artifact."},
		"include":        {"type": "string", "description": "Comma-separated extras for status: 'params' to include build parameters."},
		"downstream_job": {"type": "string", "description": "Downstream job name for the downstream action. Required — Jenkins has no native reverse index."},
		"depth":     {"type": "integer", "description": "Max recursion depth for chain (default 3, -1 = unlimited)."},
		"steps":     {"type": "boolean", "description": "Expand stages to include step-level detail (stages action)."},
		"tree":      {"type": "boolean", "description": "Return artifacts grouped as a directory tree (artifact action)."},
		"artifacts": {"type": "boolean", "description": "Attach artifact list to each node in the chain tree."}
	},
	"required": ["action"]
}`)

var (
	errUnknownAction   = errors.New("unknown action")
	errBackendRequired = errors.New("backend parameter is required")
	errJobRefRequired  = errors.New("job_ref parameter is required")
)

type ciArgs struct {
	Action   string            `json:"action"`
	Backend  string            `json:"backend"`
	JobRef   string            `json:"job_ref"`
	RunID    string            `json:"run_id"`
	QueueID   string            `json:"queue_id"`
	OpaqueRef string            `json:"opaque_ref"`
	Pipeline string            `json:"pipeline"`
	Step     int               `json:"step"`
	Params   map[string]string `json:"params"`
	Result   string            `json:"result"`
	Runner   string            `json:"runner"`
	Since    string            `json:"since"`
	Limit    int               `json:"limit"`
	Owned    bool              `json:"owned"`
	Path     string            `json:"path"`
	Tail     int               `json:"tail"`
	Grep     string            `json:"grep"`
	Include       string            `json:"include"`
	DownstreamJob string            `json:"downstream_job"`
	Depth         int               `json:"depth"`
	Steps         bool              `json:"steps"`
	Tree          bool              `json:"tree"`
	Artifacts     bool              `json:"artifacts"`
}

// ContyService combines the pipeline and CI monitor service interfaces.
type ContyService interface {
	driver.PipelineService
	driver.CIMonitorService
}

// Compile-time check that app.Service satisfies ContyService via the driver ports.
var _ ContyService = (ContyService)(nil)

// Serve runs the MCP server over stdio.
func Serve(svc ContyService) error {
	return buildServer(svc).Serve(context.Background(), &sdkmcp.StdioTransport{})
}

// ServeHTTP runs the MCP server over HTTP at the given address.
func ServeHTTP(svc ContyService, addr string) error {
	h := NewHTTPHandler(svc)
	log.Printf("conty MCP listening on %s", addr)
	return http.ListenAndServe(addr, h)
}

// NewMCPServer returns a stdio MCP server.
func NewMCPServer(svc ContyService) *sdkmcp.Server {
	return buildServer(svc).SDK()
}

// NewBatteryServer returns the underlying battery server (for testing with in-memory transports).
func NewBatteryServer(svc ContyService) *mcpserver.Server {
	return buildServer(svc)
}

// NewHTTPHandler returns the stateless HTTP handler for the MCP server.
func NewHTTPHandler(svc ContyService) http.Handler {
	opts := &sdkmcp.StreamableHTTPOptions{Stateless: true}
	return sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return buildServer(svc).SDK()
	}, opts)
}

func buildServer(svc ContyService) *mcpserver.Server {
	meta := battserver.ToolMeta{
		Name:        serverName,
		Description: "CI/CD operations — help | status | log | search | trigger | wait | artifact | cancel | upstream | downstream. " +
		"search supports params={key:value} filtering to find builds by parameter value (e.g. VERSION, HOST). " +
		"Use search before curl.",
		Keywords:    []string{"ci", "build", "deploy", "jenkins", "pipeline", "log", "trigger", "status"},
		Categories:  []string{"ci", "deployment"},
	}

	srv := mcpserver.NewServer(serverName, Version).
		WithInstructions(serverInstructions)

	srv.ToolWithSchema(meta, contySchema,
		func(ctx context.Context, input json.RawMessage) (tool.Result, error) {
			var args ciArgs
			if err := json.Unmarshal(input, &args); err != nil {
				return tool.Result{}, fmt.Errorf("invalid input: %w", err)
			}

			switch args.Action {

			case "help":
				return handleHelp(svc)

			case "status":
				if args.Pipeline != "" {
					run, err := svc.GetPipelineStatus(ctx, args.Pipeline)
					if err != nil {
						return tool.Result{}, err
					}
					return battserver.JSONResult(run)
				}
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				verdict, err := svc.GetVerdict(ctx, args.Backend, args.JobRef, args.RunID, domain.LogFilter{Tail: args.Tail, Grep: args.Grep})
				if err != nil {
					return tool.Result{}, err
				}
				out := map[string]any{"verdict": verdict}
				if strings.Contains(args.Include, "params") && verdict.Check.RunID != "" {
					params, truncatedKeys, perr := svc.CIParamsTruncated(ctx, args.Backend, args.JobRef, verdict.Check.RunID)
					if perr == nil {
						out["params"] = params
						if len(truncatedKeys) > 0 {
							out["truncated_param_keys"] = truncatedKeys
						}
					}
				}
				return battserver.JSONResult(out)

			case "log":
				f := domain.LogFilter{Tail: args.Tail, Grep: args.Grep}
				if args.Pipeline != "" {
					res, err := svc.GetStepLog(ctx, args.Pipeline, args.Step, f)
					if err != nil {
						return tool.Result{}, err
					}
					return battserver.JSONResult(res)
				}
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				res, err := svc.CILog(ctx, args.Backend, args.JobRef, args.RunID, f)
				if err != nil {
					return tool.Result{}, err
				}
				return battserver.JSONResult(res)

			case "search":
				if args.Owned {
					return battserver.JSONResult(map[string]any{"builds": svc.ListOwnedRuns()})
				}
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				f := domain.BuildFilter{
					Result: args.Result,
					Params: args.Params,
					Runner: args.Runner,
					Limit:  args.Limit,
				}
				if args.Since != "" {
					t, err := time.Parse(time.RFC3339, args.Since)
					if err != nil {
						return tool.Result{}, fmt.Errorf("invalid since (want RFC3339): %w", err)
					}
					f.Since = t
				}
				builds, err := svc.CISearch(ctx, args.Backend, args.JobRef, f)
				if err != nil {
					return tool.Result{}, err
				}
				return battserver.JSONResult(map[string]any{"builds": builds})

			case "trigger":
				if args.Pipeline != "" {
					run, err := svc.TriggerPipeline(ctx, args.Pipeline)
					if err != nil {
						return tool.Result{}, err
					}
					return battserver.JSONResult(run)
				}
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
				return battserver.JSONResult(result)

			case "wait":
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				// opaque_ref is the new name; queue_id is kept for backward compat.
				opaqueRef := args.OpaqueRef
				if opaqueRef == "" {
					opaqueRef = args.QueueID
				}
				if opaqueRef != "" {
					buildNum, err := svc.CIPoll(ctx, args.Backend, opaqueRef)
					if err != nil {
						return tool.Result{}, err
					}
					return battserver.JSONResult(map[string]string{"build_number": buildNum})
				}
				if args.RunID != "" {
					if args.JobRef == "" {
						return tool.Result{}, errJobRefRequired
					}
					status, err := svc.CIWatch(ctx, args.Backend, args.JobRef, args.RunID)
					if err != nil {
						return tool.Result{}, err
					}
					return battserver.JSONResult(status)
				}
				return tool.Result{}, fmt.Errorf("wait requires queue_id (resolve) or run_id (watch)")

			case "artifact":
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				if args.RunID == "" {
					return tool.Result{}, fmt.Errorf("run_id is required for artifact")
				}
				if args.Path == "" {
					if args.Tree {
						tree, err := svc.CIArtifactTree(ctx, args.Backend, args.JobRef, args.RunID)
						if err != nil {
							return tool.Result{}, err
						}
						return battserver.JSONResult(tree)
					}
					artifacts, err := svc.CIArtifacts(ctx, args.Backend, args.JobRef, args.RunID)
					if err != nil {
						return tool.Result{}, err
					}
					return battserver.JSONResult(map[string]any{"artifacts": artifacts})
				}
				res, err := svc.CIArtifactText(ctx, args.Backend, args.JobRef, args.RunID, args.Path, domain.LogFilter{Tail: args.Tail, Grep: args.Grep})
				if err != nil {
					return tool.Result{}, err
				}
				return battserver.JSONResult(res)

			case "cancel":
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				if args.RunID == "" {
					return tool.Result{}, fmt.Errorf("run_id is required for cancel")
				}
				if !svc.OwnsRun(args.Backend, args.RunID) {
					return tool.Result{}, fmt.Errorf("run %s was not started by this session", args.RunID)
				}
				if err := svc.CICancel(ctx, args.Backend, args.JobRef, args.RunID); err != nil {
					return tool.Result{}, err
				}
				return battserver.JSONResult(map[string]string{"status": "cancelled", "run_id": args.RunID})

			case "upstream":
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				if args.RunID == "" {
					return tool.Result{}, fmt.Errorf("run_id is required for upstream action")
				}
				run, err := svc.CIGetRun(ctx, args.Backend, args.JobRef, args.RunID)
				if err != nil {
					return tool.Result{}, err
				}
				if run.UpstreamJob == "" {
					return tool.Result{}, fmt.Errorf("no upstream cause found for %s #%s", args.JobRef, args.RunID)
				}
				return battserver.JSONResult(map[string]any{
					"upstream_job":    run.UpstreamJob,
					"upstream_run_id": run.UpstreamRunID,
					"job_ref":         args.JobRef,
					"run_id":          args.RunID,
				})

			case "stages":
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				if args.Steps {
					nodes, err := svc.CIStageTree(ctx, args.Backend, args.JobRef, args.RunID)
					if err != nil {
						return tool.Result{}, err
					}
					return battserver.JSONResult(map[string]any{"stages": nodes})
				}
				// Flat stage list — steps omitted.
				stages, err := svc.CIStageTree(ctx, args.Backend, args.JobRef, args.RunID)
				if err != nil {
					return tool.Result{}, err
				}
				// Strip steps for flat view.
				type flatStage struct {
					ID       string          `json:"id"`
					Name     string          `json:"name"`
					Status   domain.RunStatus `json:"status"`
					Duration int64           `json:"duration,omitempty"`
				}
				flat := make([]flatStage, len(stages))
				for i, s := range stages {
					flat[i] = flatStage{ID: s.ID, Name: s.Name, Status: s.Status, Duration: s.Duration}
				}
				return battserver.JSONResult(map[string]any{"stages": flat})

			case "chain":
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				depth := args.Depth
				if depth == 0 {
					depth = 3
				}
				node, err := svc.CIChain(ctx, args.Backend, args.JobRef, args.RunID, depth, args.Artifacts)
				if err != nil {
					return tool.Result{}, err
				}
				return battserver.JSONResult(node)

			case "downstream":
				if args.Backend == "" {
					return tool.Result{}, errBackendRequired
				}
				if args.JobRef == "" {
					return tool.Result{}, errJobRefRequired
				}
				if args.RunID == "" {
					return tool.Result{}, fmt.Errorf("run_id is required for downstream action")
				}
				if args.DownstreamJob == "" {
					return tool.Result{}, fmt.Errorf("downstream_job is required for downstream action")
				}
				runs, err := svc.CIDownstream(ctx, args.Backend, args.DownstreamJob, args.JobRef, args.RunID)
				if err != nil {
					return tool.Result{}, err
				}
				return battserver.JSONResult(map[string]any{"builds": runs})

			default:
				return tool.Result{}, fmt.Errorf("%w: %s", errUnknownAction, args.Action)
			}
		},
	)

	return srv
}

func handleHelp(svc ContyService) (tool.Result, error) {
	var b strings.Builder

	backends := svc.BackendInfo()
	if len(backends) > 0 {
		fmt.Fprintln(&b, "Backends:")
		for _, bi := range backends {
			line := fmt.Sprintf("  %-24s %s", bi.Name, bi.Type)
			if bi.Capabilities != "" {
				line += "  [" + bi.Capabilities + "]"
			}
			fmt.Fprintln(&b, line)
		}
		fmt.Fprintln(&b)
	}

	pipelines := svc.ListPipelines()
	if len(pipelines) > 0 {
		fmt.Fprintln(&b, "Pipelines:")
		for _, p := range pipelines {
			fmt.Fprintf(&b, "  %s\n", p)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "Actions:")
	fmt.Fprintln(&b, "  status   backend job_ref [run_id] [grep] [tail] [include=params]")
	fmt.Fprintln(&b, "           Build verdict with inline filtered failure log. run_id optional (default: latest).")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  log      backend job_ref [run_id] [grep] [tail]")
	fmt.Fprintln(&b, "           OR       pipeline [step]")
	fmt.Fprintln(&b, "           Filtered log. Default: last 200 lines. grep= narrows to matching lines only.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  search   backend job_ref [result] [runner] [since] [limit] [params]")
	fmt.Fprintln(&b, "           OR       owned=true")
	fmt.Fprintln(&b, "           Find builds. result: SUCCESS | FAILURE | ABORTED. owned=true lists this session's builds.")
	fmt.Fprintln(&b, "           params={key:value,...} filters to builds where all specified parameters match.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  trigger  backend job_ref [params]")
	fmt.Fprintln(&b, "           OR       pipeline")
	fmt.Fprintln(&b, "           Start a build or pipeline. Returns run_id and queue_id.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  wait     backend queue_id          — resolve queue to build_number")
	fmt.Fprintln(&b, "           backend job_ref run_id   — watch until terminal, returns status+progress")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  artifact backend job_ref run_id [path] [grep] [tail]")
	fmt.Fprintln(&b, "           Omit path to list artifacts. Provide path to read a text artifact.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  cancel   backend job_ref run_id")
	fmt.Fprintln(&b, "           Abort a build (only builds triggered by this session).")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  upstream backend job_ref run_id")
	fmt.Fprintln(&b, "           Return upstream_job and upstream_run_id from the build's cause chain.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  downstream backend job_ref run_id downstream_job")
	fmt.Fprintln(&b, "           Find builds in downstream_job triggered by job_ref#run_id.")
	fmt.Fprintln(&b, "           downstream_job required — Jenkins has no native reverse index.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  stages   backend job_ref [run_id] [steps]")
	fmt.Fprintln(&b, "           List pipeline stages. steps=true expands each stage to include step-level detail.")
	fmt.Fprintln(&b, "           Without steps: flat list. With steps: stage→steps tree in one call.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  chain    backend job_ref run_id [depth] [artifacts]")
	fmt.Fprintln(&b, "           Fetch a build and recursively expand its child jobs into a tree.")
	fmt.Fprintln(&b, "           depth default 3, -1 = unlimited. artifacts=true attaches artifact list to each node.")
	fmt.Fprintln(&b, "           Use instead of repeated downstream calls when child job names are unknown.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  artifact backend job_ref run_id [path] [grep] [tail] [tree]")
	fmt.Fprintln(&b, "           tree=true groups artifacts into a directory tree by relativePath (includes sizes).")
	fmt.Fprintln(&b, "           Omit path and tree to list artifacts flat. Provide path to read a text artifact.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Tips:")
	fmt.Fprintln(&b, "  status(grep=error)       — failure context in one call")
	fmt.Fprintln(&b, "  log(grep=fatal|error)    — targeted line search without status overhead")
	fmt.Fprintln(&b, "  trigger → wait → status  — full deploy-and-diagnose flow")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Notes:")
	fmt.Fprintln(&b, "  grep= accepts a regexp (e.g. 'FAILED|fatal|error'). Invalid regexp falls back to literal.")
	fmt.Fprintln(&b, "  grep applies to the full log; tail then limits the count of matching lines from the end.")
	fmt.Fprintln(&b, "  When grep is set, tail defaults to unlimited (all matches). Set tail=N to cap results.")
	fmt.Fprintln(&b, "  log fetches the full consoleText before applying grep/tail (Jenkins API limitation).")
	fmt.Fprintln(&b, "  For 400k+ byte logs use grep= to reduce returned lines, but expect a slow first fetch.")
	fmt.Fprintln(&b, "  Child build IDs (job_ref, run_id) appear in the 'children' field of status/search")
	fmt.Fprintln(&b, "  results when Jenkins encodes them in the description HTML. Use artifact or log")
	fmt.Fprintln(&b, "  directly with those job_ref/run_id values, or use downstream for explicit lookup.")

	return tool.TextResult(b.String()), nil
}

func init() {
	log.SetFlags(0)
}
