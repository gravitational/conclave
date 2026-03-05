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

var refinePlan bool

var planCmd = &cobra.Command{
	Use:   "plan [path]",
	Short: "Analyze codebase and create a plan",
	Long: `Analyze the codebase at the given path (or current directory) and create
a detailed plan breaking it down into subsystems for security analysis.

Use --refine to take an existing plan and subdivide each subsystem further,
creating a more granular plan for deeper analysis.`,
	Args:    cobra.MaximumNArgs(1),
	PreRunE: validateProvidersPreRun,
	RunE:    runPlan,
}

func init() {
	planCmd.Flags().BoolVar(&refinePlan, "refine", false, "Refine existing plan by subdividing each subsystem")
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

	// Reset session usage tracking
	agent.GlobalSession.Reset()

	// Load existing plan if refining
	var existingPlan *state.Plan
	if refinePlan {
		existingPlan, err = st.LoadMostRecentPlan()
		if err != nil {
			return fmt.Errorf("failed to load existing plan for refinement: %w", err)
		}
	}

	if refinePlan {
		display.PrintHeader("REFINE PLAN")
		display.PrintStatus("Refining: %s (%d subsystems)", existingPlan.Name, len(existingPlan.Subsystems))
	} else {
		display.PrintHeader("PLAN")
	}
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

	var prompt string
	var statusMsg string
	if refinePlan {
		prompt = generator.BuildRefinePrompt(existingPlan)
		statusMsg = "Refining subsystems"
	} else {
		prompt = generator.BuildPrompt(absPath)
		statusMsg = "Analyzing codebase"
	}

	output := agent.StreamSilent(ag, prompt, statusMsg)

	p, err := generator.ParseAndSave(output, absPath)
	if err != nil {
		return fmt.Errorf("failed to generate plan: %w", err)
	}

	fmt.Println()
	if refinePlan {
		display.PrintSuccess("Plan refined: %s", p.Name)
		display.PrintStatus("Subsystems: %d → %d", len(existingPlan.Subsystems), len(p.Subsystems))
	} else {
		display.PrintSuccess("Plan created: %s", p.Name)
		display.PrintStatus("Subsystems: %d identified", len(p.Subsystems))
	}
	for _, sub := range p.Subsystems {
		display.PrintStatus("  • %s", sub.Name)
	}
	fmt.Println()
	display.PrintSuccess("Saved: %s", st.PlanPath(p.ID, p.Slug()))

	// Print session usage summary
	printPlanUsageSummary()

	return nil
}

// printPlanUsageSummary prints a summary of token usage for the plan command
func printPlanUsageSummary() {
	total := agent.GlobalSession.GetTotal()

	// Skip if no usage recorded
	if total.TotalTokens == 0 && total.CostUSD == 0 {
		return
	}

	fmt.Println()
	// Show basic usage
	if total.TotalTokens > 0 {
		display.PrintStatus("Usage: %s ($%.2f)",
			formatTokenCount(total.TotalTokens),
			total.CostUSD)
	} else if total.CostUSD > 0 {
		// Cost recorded but no token count (shouldn't happen normally)
		display.PrintStatus("Usage: $%.2f", total.CostUSD)
	}

	// Show cache breakdown if significant
	if total.CacheReadTokens > 0 || total.CacheWriteTokens > 0 {
		display.PrintStatus("  Cache: %s read, %s created",
			formatTokenCount(total.CacheReadTokens),
			formatTokenCount(total.CacheWriteTokens))
	}
}
