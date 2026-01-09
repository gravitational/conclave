package cli

import (
	"fmt"
	"sync"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/convene"
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

	printStatus("Using plan: %s (%s)", p.Name, p.ID)

	// Load perspectives
	perspectives, err := st.LoadPerspectives(p.ID, conveneSubsystem)
	if err != nil {
		return fmt.Errorf("failed to load perspectives: %w", err)
	}

	if len(perspectives) == 0 {
		return fmt.Errorf("no perspectives found for subsystem %s - run 'conclave assess' first", conveneSubsystem)
	}

	printStatus("Loaded %d perspectives for subsystem: %s", len(perspectives), conveneSubsystem)
	printStatus("")

	// Create agents
	createAgent := CreateAgent
	printStatus("Using %s CLI for debate...", AgentBackend())

	// Generate debate prompts
	debateGen := convene.NewDebateGenerator(createAgent())
	prompts, err := debateGen.GeneratePrompts(p, conveneSubsystem, perspectives)
	if err != nil {
		return fmt.Errorf("failed to generate debate prompts: %w", err)
	}

	printStatus("")
	printStatus("Starting debate with 3 agents...")
	printStatus("")

	// Run 3 agents in parallel
	var wg sync.WaitGroup
	debates := make([]string, 3)
	colors := []string{agent.ColorMagenta, agent.ColorCyan, agent.ColorYellow}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ag := createAgent()
			prefix := fmt.Sprintf("Debater %d", idx+1)

			debates[idx] = agent.StreamWithPrefix(ag, prompts[idx], prefix, colors[idx])
		}(i)
	}

	wg.Wait()

	printStatus("")
	printStatus("Debate complete!")

	// Save debate outputs
	for i, content := range debates {
		path, err := st.SaveDebate(p.ID, conveneSubsystem, i+1, content)
		if err != nil {
			printError("failed to save debate %d: %v", i+1, err)
			continue
		}
		printStatus("Saved debate %d to: %s", i+1, path)
	}

	return nil
}
