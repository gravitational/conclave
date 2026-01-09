package cli

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/assess"
	"github.com/rob-picard-teleport/conclave/internal/convene"
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

	// Create agent factory
	var createAgent func() agent.Agent
	var agentName string
	if UseClaude() {
		createAgent = func() agent.Agent { return agent.NewClaudeAgent() }
		agentName = "Claude"
	} else {
		createAgent = func() agent.Agent { return agent.NewCodexAgent() }
		agentName = "Codex"
	}

	printStatus("=== CONCLAVE FULL AUDIT ===")
	printStatus("Using %s CLI", agentName)
	printStatus("")

	// STEP 1: Plan (or load existing)
	printStatus("=== STEP 1: PLAN ===")
	var p *state.Plan
	p, err = st.LoadMostRecentPlan()
	if err != nil {
		printStatus("No existing plan found, creating new plan...")
		printStatus("")

		generator := plan.NewGenerator(createAgent(), st)
		p, err = generator.Generate(absPath)
		if err != nil {
			return fmt.Errorf("failed to generate plan: %w", err)
		}
	} else {
		printStatus("Using existing plan: %s (%s)", p.Name, p.ID[:8])
	}

	printStatus("")
	printStatus("Plan: %s", p.Name)
	printStatus("Subsystems: %d", len(p.Subsystems))
	printStatus("")

	// STEP 2: Assess random subsystem
	printStatus("=== STEP 2: ASSESS ===")
	rand.Seed(time.Now().UnixNano())
	subsystem := &p.Subsystems[rand.Intn(len(p.Subsystems))]
	printStatus("Selected subsystem: %s", subsystem.Name)
	printStatus("")

	// Generate assessment prompts
	promptGen := assess.NewPromptGenerator(createAgent())
	prompts, err := promptGen.GeneratePrompts(p, subsystem)
	if err != nil {
		return fmt.Errorf("failed to generate prompts: %w", err)
	}

	printStatus("Running 3 parallel assessment agents...")
	printStatus("")

	var wg sync.WaitGroup
	perspectives := make([]string, 3)
	colors := []string{agent.ColorRed, agent.ColorGreen, agent.ColorBlue}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ag := createAgent()
			prefix := fmt.Sprintf("Agent %d", idx+1)
			perspectives[idx] = agent.StreamWithPrefix(ag, prompts[idx], prefix, colors[idx])
		}(i)
	}
	wg.Wait()

	// Save perspectives
	for i, content := range perspectives {
		st.SavePerspective(p.ID, subsystem.Slug, i+1, content)
	}

	printStatus("")
	printStatus("Assessment complete!")
	printStatus("")

	// STEP 3: Convene
	printStatus("=== STEP 3: CONVENE ===")
	printStatus("Running 3 parallel debate agents...")
	printStatus("")

	debateGen := convene.NewDebateGenerator(createAgent())
	debatePrompts, err := debateGen.GeneratePrompts(p, subsystem.Slug, perspectives)
	if err != nil {
		return fmt.Errorf("failed to generate debate prompts: %w", err)
	}

	debates := make([]string, 3)
	debateColors := []string{agent.ColorMagenta, agent.ColorCyan, agent.ColorYellow}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ag := createAgent()
			prefix := fmt.Sprintf("Debater %d", idx+1)
			debates[idx] = agent.StreamWithPrefix(ag, debatePrompts[idx], prefix, debateColors[idx])
		}(i)
	}
	wg.Wait()

	// Save debates
	for i, content := range debates {
		st.SaveDebate(p.ID, subsystem.Slug, i+1, content)
	}

	printStatus("")
	printStatus("Debate complete!")
	printStatus("")

	// STEP 4: Complete
	printStatus("=== STEP 4: COMPLETE ===")
	printStatus("Synthesizing final results...")
	printStatus("")

	synthesisPrompt := generateSynthesisPrompt(p, subsystem, debates)
	result := agent.StreamWithPrefix(createAgent(), synthesisPrompt, "Synthesis", agent.ColorWhite)

	// Save result
	resultPath, err := st.SaveResult(p.ID, subsystem.Slug, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	printStatus("")
	printStatus("=== AUDIT COMPLETE ===")
	printStatus("Subsystem: %s", subsystem.Name)
	printStatus("Results saved to: %s", resultPath)

	return nil
}
