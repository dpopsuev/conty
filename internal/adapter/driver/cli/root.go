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
	"github.com/dpopsuev/conty/internal/port/driven/driventest"
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
		verdict, err := svc.GetVerdict(cmd.Context(), backend, args[0])
		if err != nil {
			return err
		}
		return printJSON(verdict)
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server",
	RunE: func(_ *cobra.Command, _ []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		return mcpserver.Serve(svc)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "config file path")
	checkCmd.Flags().StringP("backend", "b", "", "backend name")
	rootCmd.AddCommand(deployCmd, checkCmd, serveCmd)
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

	adapters, warnings := adapterdriven.CreateFromConfig(cfg)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}

	svc := app.NewService(adapters...)

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
	stub := driventest.NewStubCIAdapter("stub")
	stub.RunID = "stub-run-1"
	stub.Run = &domain.CIRun{
		ID:     "stub-run-1",
		Name:   "stub-job",
		Status: domain.RunStatusSuccess,
		Result: domain.RunResultSuccess,
	}

	svc := app.NewService(stub)
	svc.RegisterPipeline(domain.Pipeline{
		Name:    "lab-deploy",
		Backend: "stub",
		Steps: []domain.PipelineStep{
			{JobName: "step-1"},
			{JobName: "step-2"},
			{JobName: "step-3"},
		},
	})

	return svc, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
