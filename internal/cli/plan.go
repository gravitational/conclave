package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/plan"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan [path]",
	Short: "Analyze codebase and create a plan",
	Long: `Analyze the codebase at the given path (or current directory) and create
a detailed plan breaking it down into subsystems for security analysis.`,
	Args:    cobra.MaximumNArgs(1),
	PreRunE: validateProvidersPreRun,
	RunE:    runPlan,
}

func init() {
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	// Determine codebase path
	codebasePath := "."
	if len(args) > 0 {
		codebasePath = args[0]
	}

	absPath, err := filepath.Abs(codebasePath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path does not exist: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path must be a directory: %s", absPath)
	}

	// Initialize state directory
	st, err := state.New(absPath)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	display.PrintHeader("PLAN")
	display.PrintStatus("Target: %s", absPath)

	// Create agent based on runtime config or CLI flags
	var ag agent.Agent
	cfg := GetRuntimeConfig()
	if cfg != nil && cfg.IsConfigured() {
		display.PrintStatus("Provider: %s", cfg.PrimaryBackend())
		ag = cfg.PlanAgent()
	} else {
		display.PrintStatus("Provider: %s", PrimaryBackend())
		ag = CreateAgent()
	}
	fmt.Println()

	// Generate plan
	generator := plan.NewGenerator(ag, st)
	output := agent.StreamSilent(ag, generator.BuildPrompt(absPath), "Analyzing codebase")

	p, err := generator.ParseAndSave(output, absPath)
	if err != nil {
		return fmt.Errorf("failed to generate plan: %w", err)
	}

	fmt.Println()
	display.PrintSuccess("Plan created: %s", p.Name)
	display.PrintStatus("Subsystems: %d identified", len(p.Subsystems))
	for _, sub := range p.Subsystems {
		display.PrintStatus("  • %s", sub.Name)
	}
	fmt.Println()
	display.PrintSuccess("Saved: %s", st.PlanPath(p.ID, p.Slug()))

	return nil
}
