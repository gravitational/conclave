package cli

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/assess"
	"github.com/rob-picard-teleport/conclave/internal/convene"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/plan"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [path]",
	Short: "Run the full audit pipeline end-to-end",
	Long: `Run the complete conclave pipeline on a codebase:
1. Create a plan (or use existing)
2. Assess a random subsystem
3. Convene agents to debate
4. Complete with final synthesis

This is equivalent to running: plan → assess → convene → complete`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFull,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runFull(cmd *cobra.Command, args []string) error {
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

	// Initialize state
	st, err := state.New(absPath)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	display.PrintHeader("CONCLAVE AUDIT")
	display.PrintStatus("Providers: %s", AgentBackend())
	display.PrintStatus("Target: %s", absPath)

	// STEP 1: Plan (or load existing)
	display.PrintHeader("STEP 1: PLAN")
	var p *state.Plan
	p, err = st.LoadMostRecentPlan()
	if err != nil {
		display.PrintStatus("Creating new plan...")
		generator := plan.NewGenerator(CreateAgent(), st)
		output := agent.StreamSilent(CreateAgent(), generator.BuildPrompt(absPath), "Analyzing codebase")
		p, err = generator.ParseAndSave(output, absPath)
		if err != nil {
			return fmt.Errorf("failed to generate plan: %w", err)
		}
		display.PrintSuccess("Plan created: %s", p.Name)
	} else {
		display.PrintSuccess("Using existing plan: %s", p.Name)
	}
	display.PrintStatus("Subsystems: %d identified", len(p.Subsystems))

	// STEP 2: Assess random subsystem
	display.PrintHeader("STEP 2: ASSESS")
	rand.Seed(time.Now().UnixNano())
	subsystem := &p.Subsystems[rand.Intn(len(p.Subsystems))]
	display.PrintStatus("Target subsystem: %s", subsystem.Name)
	fmt.Println()

	// Generate assessment prompts
	promptGen := assess.NewPromptGenerator(CreateAgent())
	prompts, err := promptGen.GeneratePrompts(p, subsystem)
	if err != nil {
		return fmt.Errorf("failed to generate prompts: %w", err)
	}

	// Run 3 assessment agents with status display
	assessAgents := DistributeAgents(3)
	names := []string{"Assessor 1", "Assessor 2", "Assessor 3"}
	perspectives := agent.StreamMultipleWithStatus(assessAgents, prompts, names)

	// Save perspectives
	for i, content := range perspectives {
		st.SavePerspective(p.ID, subsystem.Slug, i+1, content)
	}
	fmt.Println()
	display.PrintSuccess("Assessment complete")

	// STEP 3: Convene
	display.PrintHeader("STEP 3: CONVENE")
	display.PrintStatus("Agents will debate and refine findings")
	fmt.Println()

	debateGen := convene.NewDebateGenerator(CreateAgent())
	debatePrompts, err := debateGen.GeneratePrompts(p, subsystem.Slug, perspectives)
	if err != nil {
		return fmt.Errorf("failed to generate debate prompts: %w", err)
	}

	// Run 3 debate agents with status display
	debateAgents := DistributeAgents(3)
	debateNames := []string{"Debater 1", "Debater 2", "Debater 3"}
	debates := agent.StreamMultipleWithStatus(debateAgents, debatePrompts, debateNames)

	// Save debates
	for i, content := range debates {
		st.SaveDebate(p.ID, subsystem.Slug, i+1, content)
	}
	fmt.Println()
	display.PrintSuccess("Debate complete")

	// STEP 4: Complete
	display.PrintHeader("STEP 4: SYNTHESIZE")
	synthesisPrompt := generateSynthesisPrompt(p, subsystem, debates)
	result := agent.StreamSilent(CreateAgent(), synthesisPrompt, "Synthesizing findings")

	// Save result
	resultPath, err := st.SaveResult(p.ID, subsystem.Slug, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	display.PrintHeader("AUDIT COMPLETE")
	display.PrintSuccess("Subsystem: %s", subsystem.Name)
	display.PrintSuccess("Results: %s", resultPath)

	return nil
}
