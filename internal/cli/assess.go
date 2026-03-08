package cli

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/assess"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/spf13/cobra"
)

var (
	assessPlanID    string
	assessSubsystem string
)

var assessCmd = &cobra.Command{
	Use:     "assess",
	Aliases: []string{"ass"},
	Short:   "Assess a subsystem for security vulnerabilities",
	Long: `Pick a random subsystem from the plan and spin up three agents in parallel
to review it for critical security vulnerabilities, logic flaws, and other
serious issues.`,
	PreRunE: validateProvidersPreRun,
	RunE:    runAssess,
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

	// Set working directory for agent subprocesses from plan
	if p.CodebaseRoot != "" {
		agent.GlobalWorkDir = p.CodebaseRoot
	}

	display.PrintHeader("ASSESS")
	display.PrintStatus("Plan: %s", p.Name)

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
		rand.Seed(time.Now().UnixNano())
		idx := rand.Intn(len(p.Subsystems))
		subsystem = &p.Subsystems[idx]
	}

	display.PrintStatus("Subsystem: %s", subsystem.Name)

	// Get runtime config and determine agents
	cfg := GetRuntimeConfig()
	var agents []agent.Agent
	var agentCount int

	if cfg != nil && cfg.IsConfigured() {
		display.PrintStatus("Providers: %s", cfg.AgentBackend())
		agents = cfg.AssessAgents()
		agentCount = len(agents)
	} else {
		display.PrintStatus("Providers: %s", AgentBackend())
		agentCount = 3
		agents = DistributeAgents(agentCount)
	}
	fmt.Println()

	// Generate assessment prompts
	promptGen := assess.NewPromptGenerator()
	if cfg != nil {
		promptGen.WithInstructions(cfg.Instructions)
	}
	prompts, err := promptGen.GeneratePromptsN(p, subsystem, agentCount)
	if err != nil {
		return fmt.Errorf("failed to generate assessment prompts: %w", err)
	}

	// Build agent names
	names := make([]string, agentCount)
	for i := 0; i < agentCount; i++ {
		names[i] = fmt.Sprintf("Assessor %d", i+1)
	}
	results := agent.StreamMultipleWithStatus(agents, prompts, names)

	fmt.Println()
	display.PrintSuccess("Assessment complete")

	// Save perspectives with agent metadata
	for i, result := range results {
		agentMeta := state.AgentMeta{
			Provider: result.Agent.Provider,
			Model:    result.Agent.Model,
		}
		path, err := st.SavePerspective(p.ID, subsystem.Slug, i+1, agentMeta, result.Content)
		if err != nil {
			display.PrintError("Failed to save perspective %d: %v", i+1, err)
			continue
		}
		display.PrintSuccess("Saved: %s", path)
	}

	return nil
}
