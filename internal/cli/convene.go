package cli

import (
	"fmt"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/convene"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/spf13/cobra"
)

var (
	convenePlanID    string
	conveneSubsystem string
)

var conveneCmd = &cobra.Command{
	Use:   "convene",
	Short: "Have agents debate and refine their perspectives",
	Long: `Load the perspectives from an assessment and spin up three agents to
debate and improve upon the findings.`,
	RunE: runConvene,
}

func init() {
	conveneCmd.Flags().StringVar(&convenePlanID, "plan", "", "Plan UUID to use (defaults to most recent)")
	conveneCmd.Flags().StringVar(&conveneSubsystem, "subsystem", "", "Subsystem slug to convene on (required)")
	rootCmd.AddCommand(conveneCmd)
}

func runConvene(cmd *cobra.Command, args []string) error {
	if conveneSubsystem == "" {
		return fmt.Errorf("--subsystem flag is required")
	}

	// Initialize state
	st, err := state.New(".")
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	// Load plan
	var p *state.Plan
	if convenePlanID != "" {
		p, err = st.LoadPlanByID(convenePlanID)
	} else {
		p, err = st.LoadMostRecentPlan()
	}
	if err != nil {
		return fmt.Errorf("failed to load plan: %w", err)
	}

	display.PrintHeader("CONVENE")
	display.PrintStatus("Plan: %s", p.Name)
	display.PrintStatus("Subsystem: %s", conveneSubsystem)

	// Load perspectives
	perspectives, err := st.LoadPerspectives(p.ID, conveneSubsystem)
	if err != nil {
		return fmt.Errorf("failed to load perspectives: %w", err)
	}

	if len(perspectives) == 0 {
		return fmt.Errorf("no perspectives found - run 'conclave assess' first")
	}

	display.PrintStatus("Loaded %d perspectives", len(perspectives))
	display.PrintStatus("Providers: %s", AgentBackend())
	fmt.Println()

	// Generate debate prompts
	debateGen := convene.NewDebateGenerator(CreateAgent())
	prompts, err := debateGen.GeneratePrompts(p, conveneSubsystem, perspectives)
	if err != nil {
		return fmt.Errorf("failed to generate debate prompts: %w", err)
	}

	// Run 3 agents with status display
	agents := DistributeAgents(3)
	names := []string{"Debater 1", "Debater 2", "Debater 3"}
	debates := agent.StreamMultipleWithStatus(agents, prompts, names)

	fmt.Println()
	display.PrintSuccess("Debate complete")

	// Save debate outputs
	for i, content := range debates {
		path, err := st.SaveDebate(p.ID, conveneSubsystem, i+1, content)
		if err != nil {
			display.PrintError("Failed to save debate %d: %v", i+1, err)
			continue
		}
		display.PrintSuccess("Saved: %s", path)
	}

	return nil
}
