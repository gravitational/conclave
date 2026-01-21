package cli

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// toPerspectives converts agent results to perspectives
func toPerspectives(results []agent.AgentResult) []state.Perspective {
	perspectives := make([]state.Perspective, len(results))
	for i, r := range results {
		perspectives[i] = state.Perspective{
			AgentNum: i + 1,
			Agent:    state.AgentMeta{Provider: r.Agent.Provider, Model: r.Agent.Model},
			Content:  r.Content,
		}
	}
	return perspectives
}

var (
	useWeb     bool
	createGist bool
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
	Args:    cobra.MaximumNArgs(1),
	PreRunE: validateProvidersPreRun,
	RunE:    runFull,
}

func init() {
	runCmd.Flags().BoolVar(&useWeb, "web", false, "Open web dashboard for monitoring")
	runCmd.Flags().BoolVar(&createGist, "gist", false, "Create a secret gist of the final report")
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
		var streamResult agent.StreamSilentResult
		if hub != nil {
			output := agent.StreamSilentWithWeb(CreateAgent(), generator.BuildPrompt(absPath), "Analyzing codebase", hub)
			streamResult = agent.StreamSilentResult{Content: output}
		} else {
			streamResult = agent.StreamSilentWithError(CreateAgent(), generator.BuildPrompt(absPath), "Analyzing codebase")
		}
		if streamResult.Error != nil {
			return fmt.Errorf("agent error: %w", streamResult.Error)
		}
		p, err = generator.ParseAndSave(streamResult.Content, absPath)
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
	var assessResults []agent.AgentResult
	if hub != nil {
		assessResults = agent.StreamMultipleWithWeb(assessAgents, prompts, names, hub)
	} else {
		assessResults = agent.StreamMultipleWithStatus(assessAgents, prompts, names)
	}

	// Convert to perspectives and save with agent metadata
	perspectives := toPerspectives(assessResults)
	for i, result := range assessResults {
		agentMeta := state.AgentMeta{Provider: result.Agent.Provider, Model: result.Agent.Model}
		st.SavePerspective(p.ID, subsystem.Slug, i+1, agentMeta, result.Content)
	}
	fmt.Println()
	display.PrintSuccess("Assessment complete")

	// STEP 3: Adversarial Review
	display.PrintHeader("STEP 3: ADVERSARIAL REVIEW")

	// Filter to valid findings only
	findings := convene.FilterValidFindings(perspectives)
	if len(findings) == 0 {
		display.PrintStatus("No findings to debate - all assessors found no vulnerabilities")

		// Save empty result
		resultPath, err := st.SaveResult(p.ID, subsystem.Slug, "# No Vulnerabilities Found\n\nAll security assessors found no critical vulnerabilities in this subsystem.")
		if err != nil {
			return fmt.Errorf("failed to save result: %w", err)
		}

		display.PrintHeader("AUDIT COMPLETE")
		display.PrintSuccess("Subsystem: %s", subsystem.Name)
		display.PrintSuccess("Results: %s", resultPath)
		return nil
	}

	n := len(findings)
	display.PrintStatus("Valid findings: %d", n)

	debate, err := convene.NewDebate(p, subsystem.Slug)
	if err != nil {
		return fmt.Errorf("failed to create debate: %w", err)
	}
	debate.WithContext(repoCtx)

	// Run pipelined adversarial review
	display.PrintStatus("Running pipelined adversarial review (%d findings)", n)
	fmt.Println()

	// Build finding labels for display
	findingLabels := make([]string, n)
	for i, f := range findings {
		findingLabels[i] = fmt.Sprintf("Finding %d (%s)", i+1, f.Agent.Provider)
	}

	// Configure pipeline
	var pipelineDisplay *display.PipelineDisplay
	if hub == nil {
		pipelineDisplay = display.NewPipelineDisplay(n, findingLabels)
	}

	pipelineResults := agent.RunPipelinedDebate(agent.PipelineConfig{
		Debate:      debate,
		Findings:    findings,
		CreateAgent: CreateAgent,
		Hub:         hub,
		Display:     pipelineDisplay,
	})
	fmt.Println()

	// Convert pipeline results for synthesis
	steelMen := agent.ToDebateRounds(pipelineResults, agent.PhaseSteelMan)
	critiques := agent.ToDebateRounds(pipelineResults, agent.PhaseCritique)
	judges := agent.ToDebateRounds(pipelineResults, agent.PhaseJudge)

	// Parse verdicts and save
	var raiseCount, dismissCount int
	for i, res := range pipelineResults {
		if res.Error != nil {
			display.PrintError("Finding %d failed: %v", i+1, res.Error)
			continue
		}
		verdict := parseVerdict(res.Judge.Content)
		agentMeta := state.AgentMeta{Provider: res.Judge.Agent.Provider, Model: res.Judge.Agent.Model}
		st.SaveVerdict(p.ID, subsystem.Slug, i+1, agentMeta, verdict.Decision, verdict.Confidence, res.Judge.Content)

		if verdict.Decision == "RAISE" {
			raiseCount++
		} else {
			dismissCount++
		}
	}

	display.PrintStatus("Verdicts: %d RAISE, %d DISMISS", raiseCount, dismissCount)

	// Phase 4: Synthesis
	if hub != nil {
		hub.SetPhase("synthesize", "Phase 4: Synthesis")
	}
	display.PrintStatus("Phase 4: Synthesis")
	synthesisPrompt := debate.SynthesisPrompt(findings, steelMen, critiques, judges)
	var result string
	if hub != nil {
		result = agent.StreamSilentWithWeb(CreateAgent(), synthesisPrompt, "Synthesizing final report", hub)
	} else {
		result = agent.StreamSilent(CreateAgent(), synthesisPrompt, "Synthesizing final report")
	}

	// Save result
	resultPath, err := st.SaveResult(p.ID, subsystem.Slug, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	display.PrintHeader("AUDIT COMPLETE")
	display.PrintSuccess("Subsystem: %s", subsystem.Name)
	display.PrintSuccess("Results: %s", resultPath)

	// Create gist if requested
	if createGist {
		fmt.Println()
		display.PrintStatus("Creating secret gist...")
		gistURL, err := createSecretGist(resultPath, subsystem.Name)
		if err != nil {
			display.PrintError("Failed to create gist: %v", err)
		} else {
			display.PrintSuccess("Gist: %s", gistURL)
		}
	}

	// Keep web server running if dashboard is open
	if useWeb {
		fmt.Println()
		display.PrintStatus("Dashboard still running. Press Ctrl+C to exit.")
		select {} // Block forever
	}

	return nil
}

func createSecretGist(filePath, subsystemName string) (string, error) {
	// Use gh gist create (creates secret gists by default)
	cmd := exec.Command("gh", "gist", "create", "--desc",
		fmt.Sprintf("Conclave Security Audit: %s", subsystemName),
		filePath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh command failed: %w\nOutput: %s", err, string(output))
	}

	// The output should be the gist URL, trim whitespace
	gistURL := strings.TrimSpace(string(output))

	return gistURL, nil
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
