package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/conclave/internal/agent"
	"github.com/anthropics/conclave/internal/plan"
	"github.com/anthropics/conclave/internal/state"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan [path]",
	Short: "Analyze codebase and create a plan",
	Long: `Analyze the codebase at the given path (or current directory) and create
a detailed plan breaking it down into subsystems for security analysis.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlan,
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

	// Verify path exists
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

	// Create agent
	var ag agent.Agent
	if UseClaude() {
		ag = agent.NewClaudeAgent()
		printStatus("Using Claude CLI for analysis...")
	} else {
		ag = agent.NewCodexAgent()
		printStatus("Using Codex CLI for analysis...")
	}

	// Generate plan
	printStatus("Analyzing codebase at %s...", absPath)
	printStatus("")

	generator := plan.NewGenerator(ag, st)
	p, err := generator.Generate(absPath)
	if err != nil {
		return fmt.Errorf("failed to generate plan: %w", err)
	}

	printStatus("")
	printStatus("Plan created: %s", p.ID)
	printStatus("Name: %s", p.Name)
	printStatus("Subsystems identified: %d", len(p.Subsystems))
	printStatus("")
	printStatus("Plan saved to: %s", st.PlanPath(p.ID, p.Slug()))

	return nil
}
