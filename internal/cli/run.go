package cli

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/assess"
	"github.com/rob-picard-teleport/conclave/internal/context"
	"github.com/rob-picard-teleport/conclave/internal/convene"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/plan"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/rob-picard-teleport/conclave/internal/web"
	"github.com/spf13/cobra"
)

var (
	useWeb bool
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
	runCmd.Flags().BoolVar(&useWeb, "web", false, "Open web dashboard for monitoring")
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

	// Start web dashboard if requested
	var hub *web.Hub
	if useWeb {
		hub = web.NewHub()

		// Wire up agent control functions
		hub.SetControllers(
			agent.GlobalRegistry.Kill,
			agent.GlobalRegistry.KillAll,
		)

		go hub.Run()

		server := web.NewServer(hub)
		url, err := server.Start()
		if err != nil {
			return fmt.Errorf("failed to start web server: %w", err)
		}

		fmt.Printf("\n  Dashboard: %s\n\n", url)

		// Try to open browser
		openBrowser(url)
	}

	// Load repository context (CONCLAVE.md)
	repoCtx, err := context.Load(absPath)
	if err != nil {
		return fmt.Errorf("failed to load context: %w", err)
	}

	display.PrintHeader("CONCLAVE AUDIT")
	display.PrintStatus("Providers: %s", AgentBackend())
	display.PrintStatus("Target: %s", absPath)
	if repoCtx.Exists() {
		display.PrintStatus("Context: %s", repoCtx.Path())
	}

	// STEP 1: Plan (or load existing)
	if hub != nil {
		hub.SetPhase("plan", "Analyzing codebase structure")
	}
	display.PrintHeader("STEP 1: PLAN")
	var p *state.Plan
	p, err = st.LoadMostRecentPlan()
	if err != nil {
		display.PrintStatus("Creating new plan...")
		generator := plan.NewGenerator(CreateAgent(), st)
		var output string
		if hub != nil {
			output = agent.StreamSilentWithWeb(CreateAgent(), generator.BuildPrompt(absPath), "Analyzing codebase", hub)
		} else {
			output = agent.StreamSilent(CreateAgent(), generator.BuildPrompt(absPath), "Analyzing codebase")
		}
		p, err = generator.ParseAndSave(output, absPath)
		if err != nil {
			return fmt.Errorf("failed to generate plan: %w", err)
		}
		display.PrintSuccess("Plan created: %s", p.Name)
	} else {
		display.PrintSuccess("Using existing plan: %s", p.Name)
	}
	display.PrintStatus("Subsystems: %d identified", len(p.Subsystems))

	// STEP 2: Assess subsystem (prioritize unreviewed)
	if hub != nil {
		hub.SetPhase("assess", "Security assessment in progress")
	}
	display.PrintHeader("STEP 2: ASSESS")

	// Find subsystems that haven't been reviewed yet
	var unreviewed []*state.Subsystem
	var reviewed []string
	for i := range p.Subsystems {
		sub := &p.Subsystems[i]
		if result, _ := st.LoadResult(p.ID, sub.Slug); result == "" {
			unreviewed = append(unreviewed, sub)
		} else {
			reviewed = append(reviewed, sub.Name)
		}
	}

	rand.Seed(time.Now().UnixNano())
	var subsystem *state.Subsystem
	if len(unreviewed) > 0 {
		// Pick from unreviewed subsystems
		subsystem = unreviewed[rand.Intn(len(unreviewed))]
		display.PrintStatus("Progress: %d/%d subsystems reviewed", len(reviewed), len(p.Subsystems))
	} else {
		// All reviewed - pick any for re-review
		subsystem = &p.Subsystems[rand.Intn(len(p.Subsystems))]
		display.PrintStatus("All %d subsystems reviewed - re-reviewing", len(p.Subsystems))
	}
	display.PrintStatus("Target subsystem: %s", subsystem.Name)
	fmt.Println()

	// Generate assessment prompts
	promptGen := assess.NewPromptGenerator(CreateAgent()).WithContext(repoCtx)
	prompts, err := promptGen.GeneratePrompts(p, subsystem)
	if err != nil {
		return fmt.Errorf("failed to generate prompts: %w", err)
	}

	// Show generated prompts
	for i, prompt := range prompts {
		display.PrintPrompt(fmt.Sprintf("Agent %d Prompt", i+1), prompt, 15)
	}
	fmt.Println()

	// Run 3 assessment agents
	assessAgents := DistributeAgents(3)
	names := []string{"Assessor 1", "Assessor 2", "Assessor 3"}
	var perspectives []string
	if hub != nil {
		perspectives = agent.StreamMultipleWithWeb(assessAgents, prompts, names, hub)
	} else {
		perspectives = agent.StreamMultipleWithStatus(assessAgents, prompts, names)
	}

	// Save perspectives
	for i, content := range perspectives {
		st.SavePerspective(p.ID, subsystem.Slug, i+1, content)
	}
	fmt.Println()
	display.PrintSuccess("Assessment complete")

	// STEP 3: Multi-round Debate
	display.PrintHeader("STEP 3: DEBATE")

	debate, err := convene.NewDebate(p, subsystem.Slug)
	if err != nil {
		return fmt.Errorf("failed to create debate: %w", err)
	}
	debate.WithContext(repoCtx)

	// Round 1: Review initial findings
	if hub != nil {
		hub.SetPhase("debate", "Debate Round 1: Reviewing findings")
	}
	display.PrintStatus("Round 1: Reviewing initial findings")
	fmt.Println()
	round1Prompts := debate.Round1Prompts(perspectives)
	debateAgents := DistributeAgents(3)
	var round1 []string
	if hub != nil {
		round1 = agent.StreamMultipleWithWeb(debateAgents, round1Prompts, []string{"Reviewer 1", "Reviewer 2", "Reviewer 3"}, hub)
	} else {
		round1 = agent.StreamMultipleWithStatus(debateAgents, round1Prompts, []string{"Reviewer 1", "Reviewer 2", "Reviewer 3"})
	}
	fmt.Println()

	// Round 2: Respond to each other
	if hub != nil {
		hub.SetPhase("debate", "Debate Round 2: Cross-review")
	}
	display.PrintStatus("Round 2: Debating findings")
	fmt.Println()
	round2Prompts := debate.Round2Prompts(perspectives, round1)
	debateAgents2 := DistributeAgents(3)
	var round2 []string
	if hub != nil {
		round2 = agent.StreamMultipleWithWeb(debateAgents2, round2Prompts, []string{"Reviewer 1", "Reviewer 2", "Reviewer 3"}, hub)
	} else {
		round2 = agent.StreamMultipleWithStatus(debateAgents2, round2Prompts, []string{"Reviewer 1", "Reviewer 2", "Reviewer 3"})
	}
	fmt.Println()

	// Save debate rounds
	for i, content := range round1 {
		st.SaveDebate(p.ID, subsystem.Slug, i+1, content)
	}

	// Final: Synthesize into report
	if hub != nil {
		hub.SetPhase("synthesize", "Final synthesis")
	}
	display.PrintStatus("Final: Synthesizing report")
	finalPrompt := debate.FinalPrompt(perspectives, round1, round2)
	var result string
	if hub != nil {
		result = agent.StreamSilentWithWeb(CreateAgent(), finalPrompt, "Producing final report", hub)
	} else {
		result = agent.StreamSilent(CreateAgent(), finalPrompt, "Producing final report")
	}

	// Save result
	resultPath, err := st.SaveResult(p.ID, subsystem.Slug, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	display.PrintHeader("AUDIT COMPLETE")
	display.PrintSuccess("Subsystem: %s", subsystem.Name)
	display.PrintSuccess("Results: %s", resultPath)

	// Keep web server running if dashboard is open
	if useWeb {
		fmt.Println()
		display.PrintStatus("Dashboard still running. Press Ctrl+C to exit.")
		select {} // Block forever
	}

	return nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Start()
}
