package cli

import (
	"encoding/json"
	"fmt"
	"os"

	adapterdriven "github.com/dpopsuev/conty/internal/adapter/driven"
	_ "github.com/dpopsuev/conty/internal/adapter/driven/github"
	_ "github.com/dpopsuev/conty/internal/adapter/driven/gitlab"
	_ "github.com/dpopsuev/conty/internal/adapter/driven/jenkins"
	mcpserver "github.com/dpopsuev/conty/internal/adapter/driver/mcp"
	"github.com/dpopsuev/conty/internal/app"
	"github.com/dpopsuev/conty/internal/config"
	"github.com/dpopsuev/conty/internal/domain"
	"github.com/spf13/cobra"
)

var flagConfig string

var rootCmd = &cobra.Command{
	Use:   "conty",
	Short: "AI-driven CI/CD execution tool",
}

var deployCmd = &cobra.Command{
	Use:   "deploy [pipeline]",
	Short: "Trigger a pipeline deployment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		run, err := svc.TriggerPipeline(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printJSON(run)
	},
}

var checkCmd = &cobra.Command{
	Use:   "check [job-ref]",
	Short: "Get CI verdict for latest run",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backend, _ := cmd.Flags().GetString("backend")
		svc, err := newService()
		if err != nil {
			return err
		}
		verdict, err := svc.GetVerdict(cmd.Context(), backend, args[0], "", domain.LogFilter{})
		if err != nil {
			return err
		}
		return printJSON(verdict)
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server (stdio or HTTP)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		addr, _ := cmd.Flags().GetString("addr")
		if addr != "" {
			return mcpserver.ServeHTTP(svc, addr)
		}
		return mcpserver.Serve(svc)
	},
}

var stagesCmd = &cobra.Command{
	Use:   "stages [job-ref] [run-id]",
	Short: "List pipeline stages with optional step detail",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		backend, _ := cmd.Flags().GetString("backend")
		steps, _ := cmd.Flags().GetBool("steps")
		includeLogs, _ := cmd.Flags().GetBool("include-failed-log")
		runID := ""
		if len(args) > 1 {
			runID = args[1]
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		if !steps {
			nodes, err := svc.CIStageTree(cmd.Context(), backend, args[0], runID)
			if err != nil {
				return err
			}
			return printJSON(nodes)
		}
		if includeLogs {
			nodes, err := svc.CIStageTreeWithLogs(cmd.Context(), backend, args[0], runID)
			if err != nil {
				return err
			}
			return printJSON(nodes)
		}
		nodes, err := svc.CIStageTree(cmd.Context(), backend, args[0], runID)
		if err != nil {
			return err
		}
		return printJSON(nodes)
	},
}

var chainCmd = &cobra.Command{
	Use:   "chain [job-ref] [run-id]",
	Short: "Fetch full build tree (parent + child jobs)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		backend, _ := cmd.Flags().GetString("backend")
		depth, _ := cmd.Flags().GetInt("depth")
		artifacts, _ := cmd.Flags().GetBool("artifacts")
		runID := ""
		if len(args) > 1 {
			runID = args[1]
		}
		if depth == 0 {
			depth = 3
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		node, err := svc.CIChain(cmd.Context(), backend, args[0], runID, depth, artifacts)
		if err != nil {
			return err
		}
		return printJSON(node)
	},
}

var artifactTreeCmd = &cobra.Command{
	Use:   "artifact-tree [job-ref] [run-id]",
	Short: "List artifacts grouped into a directory tree",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		backend, _ := cmd.Flags().GetString("backend")
		runID := ""
		if len(args) > 1 {
			runID = args[1]
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		tree, err := svc.CIArtifactTree(cmd.Context(), backend, args[0], runID)
		if err != nil {
			return err
		}
		return printJSON(tree)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "config file path")
	checkCmd.Flags().StringP("backend", "b", "", "backend name")
	serveCmd.Flags().String("addr", "", "HTTP listen address (e.g. :8082). Omit for stdio")
	stagesCmd.Flags().StringP("backend", "b", "", "backend name")
	stagesCmd.Flags().Bool("steps", false, "expand stages to include step-level detail")
	stagesCmd.Flags().Bool("include-failed-log", false, "attach wfapi log of failed steps (requires --steps)")
	chainCmd.Flags().StringP("backend", "b", "", "backend name")
	chainCmd.Flags().Int("depth", 3, "max recursion depth (-1 = unlimited)")
	chainCmd.Flags().Bool("artifacts", false, "attach artifact list to each node")
	artifactTreeCmd.Flags().StringP("backend", "b", "", "backend name")
	rootCmd.AddCommand(deployCmd, checkCmd, serveCmd, stagesCmd, chainCmd, artifactTreeCmd)
}

func Execute() error {
	return rootCmd.Execute()
}

func newService() (*app.Service, error) {
	if config.Exists(flagConfig) {
		return newServiceFromConfig()
	}
	return newServiceFromStubs()
}

func newServiceFromConfig() (*app.Service, error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, err
	}

	adapters, unconfigured, warnings := adapterdriven.CreateFromConfig(cfg)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}

	svc := app.NewService(adapters...)
	svc.RegisterUnconfigured(unconfigured)

	for name, pcfg := range cfg.Pipelines {
		steps := make([]domain.PipelineStep, len(pcfg.Steps))
		for i, s := range pcfg.Steps {
			steps[i] = domain.PipelineStep{JobName: s.Job, Params: s.Params}
		}
		svc.RegisterPipeline(domain.Pipeline{
			Name:    name,
			Steps:   steps,
			Backend: pcfg.Backend,
		})
	}

	return svc, nil
}

func newServiceFromStubs() (*app.Service, error) {
	// No config file found. Return an empty service — backends are loaded from config only.
	// For local dev with a stub adapter, set CONTY_CONFIG or provide a config file.
	return app.NewService(), nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
