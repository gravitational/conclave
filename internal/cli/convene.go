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
	Short: "Run multi-round debate on assessment findings",
	Long: `Load the perspectives from an assessment and run a multi-round debate:
- Round 1: Agents review initial findings
- Round 2: Agents respond to each other
- Final: Synthesize into report`,
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

	// Create debate
	debate, err := convene.NewDebate(p, conveneSubsystem)
	if err != nil {
		return fmt.Errorf("failed to create debate: %w", err)
	}

	// Round 1: Review initial findings
	display.PrintStatus("Round 1: Reviewing initial findings")
	fmt.Println()
	round1Prompts := debate.Round1Prompts(perspectives)
	agents1 := DistributeAgents(3)
	round1 := agent.StreamMultipleWithStatus(agents1, round1Prompts, []string{"Reviewer 1", "Reviewer 2", "Reviewer 3"})
	fmt.Println()

	// Round 2: Respond to each other
	display.PrintStatus("Round 2: Debating findings")
	fmt.Println()
	round2Prompts := debate.Round2Prompts(perspectives, round1)
	agents2 := DistributeAgents(3)
	round2 := agent.StreamMultipleWithStatus(agents2, round2Prompts, []string{"Reviewer 1", "Reviewer 2", "Reviewer 3"})
	fmt.Println()

	// Save debate outputs
	for i, content := range round1 {
		st.SaveDebate(p.ID, conveneSubsystem, i+1, content)
	}

	// Final synthesis
	display.PrintStatus("Final: Synthesizing report")
	finalPrompt := debate.FinalPrompt(perspectives, round1, round2)
	result := agent.StreamSilent(CreateAgent(), finalPrompt, "Producing final report")

	// Save result
	resultPath, err := st.SaveResult(p.ID, conveneSubsystem, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	fmt.Println()
	display.PrintSuccess("Debate complete")
	display.PrintSuccess("Result: %s", resultPath)

	return nil
}
