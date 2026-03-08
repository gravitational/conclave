package cli

import (
	"fmt"
	"strings"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/prompts"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/spf13/cobra"
)

var (
	completePlanID    string
	completeSubsystem string
)

var completeCmd = &cobra.Command{
	Use:   "complete",
	Short: "Synthesize final results from debates",
	Long: `Review the debate outputs and synthesize the most promising findings
into a final report.`,
	PreRunE: validateProvidersPreRun,
	RunE:    runComplete,
}

func init() {
	completeCmd.Flags().StringVar(&completePlanID, "plan", "", "Plan UUID to use (defaults to most recent)")
	completeCmd.Flags().StringVar(&completeSubsystem, "subsystem", "", "Subsystem slug to complete (required)")
	rootCmd.AddCommand(completeCmd)
}

func runComplete(cmd *cobra.Command, args []string) error {
	if completeSubsystem == "" {
		return fmt.Errorf("--subsystem flag is required")
	}

	// Initialize state
	st, err := state.New(".")
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	// Load plan
	var p *state.Plan
	if completePlanID != "" {
		p, err = st.LoadPlanByID(completePlanID)
	} else {
		p, err = st.LoadMostRecentPlan()
	}
	if err != nil {
		return fmt.Errorf("failed to load plan: %w", err)
	}

	// Set working directory for agent subprocesses from plan
	if p.CodebaseRoot != "" {
		agent.GlobalWorkDir = p.CodebaseRoot
	}

	display.PrintHeader("SYNTHESIZE")
	display.PrintStatus("Plan: %s", p.Name)
	display.PrintStatus("Subsystem: %s", completeSubsystem)

	// Load debates
	debates, err := st.LoadDebates(p.ID, completeSubsystem)
	if err != nil {
		return fmt.Errorf("failed to load debates: %w", err)
	}

	if len(debates) == 0 {
		return fmt.Errorf("no debates found - run 'conclave convene' first")
	}

	display.PrintStatus("Loaded %d debate outputs", len(debates))
	cfg := GetRuntimeConfig()
	if cfg != nil && cfg.IsConfigured() {
		display.PrintStatus("Provider: %s", cfg.PrimaryBackend())
	} else {
		display.PrintStatus("Provider: %s", PrimaryBackend())
	}
	fmt.Println()

	// Find subsystem details
	var subsystem *state.Subsystem
	for i := range p.Subsystems {
		if p.Subsystems[i].Slug == completeSubsystem {
			subsystem = &p.Subsystems[i]
			break
		}
	}
	if subsystem == nil {
		return fmt.Errorf("subsystem not found in plan: %s", completeSubsystem)
	}

	// Generate synthesis
	prompt := generateSynthesisPrompt(p, subsystem, debates)
	var ag agent.Agent
	if cfg != nil && cfg.IsConfigured() {
		ag = cfg.CompleteAgent()
	} else {
		ag = CreateAgent()
	}
	result := agent.StreamSilent(ag, prompt, "Synthesizing findings")

	// Save result
	path, err := st.SaveResult(p.ID, completeSubsystem, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	fmt.Println()
	display.PrintSuccess("Results saved: %s", path)

	return nil
}

func generateSynthesisPrompt(p *state.Plan, subsystem *state.Subsystem, debates []string) string {
	var sb strings.Builder
	for i, debate := range debates {
		sb.WriteString(fmt.Sprintf("### Debater %d's Analysis\n%s\n\n", i+1, debate))
	}

	return prompts.Render(prompts.Complete, map[string]any{
		"Overview":     p.Overview,
		"Name":         subsystem.Name,
		"Paths":        subsystem.Paths,
		"Description":  subsystem.Description,
		"DebateOutputs": sb.String(),
	})
}
