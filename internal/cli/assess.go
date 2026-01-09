package cli

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/assess"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/spf13/cobra"
)

var (
	assessPlanID     string
	assessSubsystem  string
)

var assessCmd = &cobra.Command{
	Use:     "assess",
	Aliases: []string{"ass"},
	Short:   "Assess a subsystem for security vulnerabilities",
	Long: `Pick a random subsystem from the plan and spin up three agents in parallel
to review it for critical security vulnerabilities, logic flaws, and other
serious issues.`,
	RunE: runAssess,
}

func init() {
	assessCmd.Flags().StringVar(&assessPlanID, "plan", "", "Plan UUID to use (defaults to most recent)")
	assessCmd.Flags().StringVar(&assessSubsystem, "subsystem", "", "Specific subsystem slug to assess (defaults to random)")
	rootCmd.AddCommand(assessCmd)
}

func runAssess(cmd *cobra.Command, args []string) error {
	// Initialize state
	st, err := state.New(".")
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	// Load plan
	var p *state.Plan
	if assessPlanID != "" {
		p, err = st.LoadPlanByID(assessPlanID)
	} else {
		p, err = st.LoadMostRecentPlan()
	}
	if err != nil {
		return fmt.Errorf("failed to load plan: %w", err)
	}

	printStatus("Using plan: %s (%s)", p.Name, p.ID)

	// Select subsystem
	var subsystem *state.Subsystem
	if assessSubsystem != "" {
		for i := range p.Subsystems {
			if p.Subsystems[i].Slug == assessSubsystem {
				subsystem = &p.Subsystems[i]
				break
			}
		}
		if subsystem == nil {
			return fmt.Errorf("subsystem not found: %s", assessSubsystem)
		}
	} else {
		// Pick random subsystem
		rand.Seed(time.Now().UnixNano())
		idx := rand.Intn(len(p.Subsystems))
		subsystem = &p.Subsystems[idx]
	}

	printStatus("Assessing subsystem: %s", subsystem.Name)
	printStatus("")

	// Create agents
	var createAgent func() agent.Agent
	if UseClaude() {
		createAgent = func() agent.Agent { return agent.NewClaudeAgent() }
		printStatus("Using Claude CLI for assessment...")
	} else {
		createAgent = func() agent.Agent { return agent.NewCodexAgent() }
		printStatus("Using Codex CLI for assessment...")
	}

	// Generate assessment prompts using LLM
	promptGen := assess.NewPromptGenerator(createAgent())
	prompts, err := promptGen.GeneratePrompts(p, subsystem)
	if err != nil {
		return fmt.Errorf("failed to generate assessment prompts: %w", err)
	}

	printStatus("")
	printStatus("Starting 3 parallel agents...")
	printStatus("")

	// Run 3 agents in parallel
	var wg sync.WaitGroup
	perspectives := make([]*assess.Perspective, 3)
	colors := []string{agent.ColorRed, agent.ColorGreen, agent.ColorBlue}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ag := createAgent()
			prefix := fmt.Sprintf("Agent %d", idx+1)

			output := agent.StreamWithPrefix(ag, prompts[idx], prefix, colors[idx])
			perspectives[idx] = &assess.Perspective{
				AgentID:    idx + 1,
				Subsystem:  subsystem.Slug,
				Content:    output,
			}
		}(i)
	}

	wg.Wait()

	printStatus("")
	printStatus("Assessment complete!")

	// Save perspectives
	for _, persp := range perspectives {
		path, err := st.SavePerspective(p.ID, subsystem.Slug, persp.AgentID, persp.Content)
		if err != nil {
			printError("failed to save perspective %d: %v", persp.AgentID, err)
			continue
		}
		printStatus("Saved perspective %d to: %s", persp.AgentID, path)
	}

	return nil
}
