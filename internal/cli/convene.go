package cli

import (
	"fmt"
	"regexp"
	"strings"

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
	Short: "Run adversarial review on assessment findings",
	Long: `Load the perspectives from an assessment and run adversarial review:
- Steel Man: Make strongest case for each finding
- Critique: Argue against each finding
- Judge: Decide RAISE or DISMISS for each
- Synthesis: Combine verdicts into final report`,
	PreRunE: validateProvidersPreRun,
	RunE:    runConvene,
}

func init() {
	conveneCmd.Flags().StringVar(&convenePlanID, "plan", "", "Plan UUID to use (defaults to most recent)")
	conveneCmd.Flags().StringVar(&conveneSubsystem, "subsystem", "", "Subsystem slug to convene on (required)")
	rootCmd.AddCommand(conveneCmd)
}

// parsedVerdict holds the parsed components from a judge's output
type parsedVerdict struct {
	Decision   string
	Reasoning  string
	Confidence string
}

// parseVerdict extracts VERDICT, REASONING, and CONFIDENCE from judge output
func parseVerdict(content string) parsedVerdict {
	result := parsedVerdict{
		Decision:   "DISMISS", // Default if parsing fails
		Confidence: "LOW",
	}

	// Match VERDICT: RAISE or VERDICT: DISMISS
	verdictRe := regexp.MustCompile(`(?i)VERDICT:\s*(RAISE|DISMISS)`)
	if match := verdictRe.FindStringSubmatch(content); len(match) > 1 {
		result.Decision = strings.ToUpper(match[1])
	}

	// Match CONFIDENCE: HIGH/MEDIUM/LOW
	confRe := regexp.MustCompile(`(?i)CONFIDENCE:\s*(HIGH|MEDIUM|LOW)`)
	if match := confRe.FindStringSubmatch(content); len(match) > 1 {
		result.Confidence = strings.ToUpper(match[1])
	}

	// Extract reasoning (everything between REASONING: and CONFIDENCE:)
	reasonRe := regexp.MustCompile(`(?is)REASONING:\s*(.+?)(?:CONFIDENCE:|$)`)
	if match := reasonRe.FindStringSubmatch(content); len(match) > 1 {
		result.Reasoning = strings.TrimSpace(match[1])
	} else {
		// Fallback: use the whole content as reasoning
		result.Reasoning = content
	}

	return result
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

	// Filter to valid findings only
	findings := convene.FilterValidFindings(perspectives)
	if len(findings) == 0 {
		display.PrintStatus("No findings to debate - all assessors found no vulnerabilities")
		return nil
	}

	n := len(findings)
	display.PrintStatus("Valid findings: %d", n)
	display.PrintStatus("Providers: %s", AgentBackend())
	fmt.Println()

	// Create debate
	debate, err := convene.NewDebate(p, conveneSubsystem)
	if err != nil {
		return fmt.Errorf("failed to create debate: %w", err)
	}

	// Phase 1: Steel Man
	display.PrintStatus("Phase 1: Steel Man (%d findings)", n)
	fmt.Println()
	steelManPrompts := debate.SteelManPrompts(findings)
	steelManAgents := DistributeAgents(n)
	steelManNames := make([]string, n)
	for i := 0; i < n; i++ {
		steelManNames[i] = fmt.Sprintf("Advocate %d", i+1)
	}
	steelManResults := agent.StreamMultipleWithStatus(steelManAgents, steelManPrompts, steelManNames)
	steelMen := toDebateRounds(steelManResults)
	fmt.Println()

	// Phase 2: Critique
	display.PrintStatus("Phase 2: Critique (%d findings)", n)
	fmt.Println()
	critiquePrompts := debate.CritiquePrompts(findings, steelMen)
	critiqueAgents := DistributeAgents(n)
	critiqueNames := make([]string, n)
	for i := 0; i < n; i++ {
		critiqueNames[i] = fmt.Sprintf("Critic %d", i+1)
	}
	critiqueResults := agent.StreamMultipleWithStatus(critiqueAgents, critiquePrompts, critiqueNames)
	critiques := toDebateRounds(critiqueResults)
	fmt.Println()

	// Phase 3: Judge
	display.PrintStatus("Phase 3: Judge (%d findings)", n)
	fmt.Println()
	judgePrompts := debate.JudgePrompts(findings, steelMen, critiques)
	judgeAgents := DistributeAgents(n)
	judgeNames := make([]string, n)
	for i := 0; i < n; i++ {
		judgeNames[i] = fmt.Sprintf("Judge %d", i+1)
	}
	judgeResults := agent.StreamMultipleWithStatus(judgeAgents, judgePrompts, judgeNames)
	judges := toDebateRounds(judgeResults)
	fmt.Println()

	// Parse verdicts and save
	var raiseCount, dismissCount int
	for i, result := range judgeResults {
		verdict := parseVerdict(result.Content)
		agentMeta := state.AgentMeta{Provider: result.Agent.Provider, Model: result.Agent.Model}
		st.SaveVerdict(p.ID, conveneSubsystem, i+1, agentMeta, verdict.Decision, verdict.Confidence, result.Content)

		if verdict.Decision == "RAISE" {
			raiseCount++
		} else {
			dismissCount++
		}
	}

	display.PrintStatus("Verdicts: %d RAISE, %d DISMISS", raiseCount, dismissCount)

	// Phase 4: Synthesis
	display.PrintStatus("Phase 4: Synthesis")
	synthesisPrompt := debate.SynthesisPrompt(findings, steelMen, critiques, judges)
	result := agent.StreamSilent(CreateAgent(), synthesisPrompt, "Synthesizing final report")

	// Save result
	resultPath, err := st.SaveResult(p.ID, conveneSubsystem, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	fmt.Println()
	display.PrintSuccess("Adversarial review complete")
	display.PrintSuccess("Result: %s", resultPath)

	return nil
}
