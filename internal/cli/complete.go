package cli

import (
	"fmt"

	"github.com/anthropics/conclave/internal/agent"
	"github.com/anthropics/conclave/internal/state"
	"github.com/spf13/cobra"
)

var (
	completePlanID    string
	completeSubsystem string
)

var completeCmd = &cobra.Command{
	Use:   "complete",
	Short: "Synthesize final results from debates",
	Long: `Review the debate outputs and synthesize the most promising findings
into a final report.`,
	RunE: runComplete,
}

func init() {
	completeCmd.Flags().StringVar(&completePlanID, "plan", "", "Plan UUID to use (defaults to most recent)")
	completeCmd.Flags().StringVar(&completeSubsystem, "subsystem", "", "Subsystem slug to complete (required)")
	rootCmd.AddCommand(completeCmd)
}

func runComplete(cmd *cobra.Command, args []string) error {
	if completeSubsystem == "" {
		return fmt.Errorf("--subsystem flag is required")
	}

	// Initialize state
	st, err := state.New(".")
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	// Load plan
	var p *state.Plan
	if completePlanID != "" {
		p, err = st.LoadPlanByID(completePlanID)
	} else {
		p, err = st.LoadMostRecentPlan()
	}
	if err != nil {
		return fmt.Errorf("failed to load plan: %w", err)
	}

	printStatus("Using plan: %s (%s)", p.Name, p.ID)

	// Load debates
	debates, err := st.LoadDebates(p.ID, completeSubsystem)
	if err != nil {
		return fmt.Errorf("failed to load debates: %w", err)
	}

	if len(debates) == 0 {
		return fmt.Errorf("no debates found for subsystem %s - run 'conclave convene' first", completeSubsystem)
	}

	printStatus("Loaded %d debate outputs for subsystem: %s", len(debates), completeSubsystem)
	printStatus("")

	// Create agent
	var ag agent.Agent
	if UseClaude() {
		ag = agent.NewClaudeAgent()
		printStatus("Using Claude CLI for synthesis...")
	} else {
		ag = agent.NewCodexAgent()
		printStatus("Using Codex CLI for synthesis...")
	}

	// Find subsystem details
	var subsystem *state.Subsystem
	for i := range p.Subsystems {
		if p.Subsystems[i].Slug == completeSubsystem {
			subsystem = &p.Subsystems[i]
			break
		}
	}
	if subsystem == nil {
		return fmt.Errorf("subsystem not found in plan: %s", completeSubsystem)
	}

	// Generate synthesis prompt
	prompt := generateSynthesisPrompt(p, subsystem, debates)

	printStatus("")
	printStatus("Synthesizing final results...")
	printStatus("")

	// Run synthesis
	result := agent.StreamWithPrefix(ag, prompt, "Synthesis", agent.ColorWhite)

	printStatus("")
	printStatus("Synthesis complete!")

	// Save result
	path, err := st.SaveResult(p.ID, completeSubsystem, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	printStatus("Final results saved to: %s", path)

	return nil
}

func generateSynthesisPrompt(p *state.Plan, subsystem *state.Subsystem, debates []string) string {
	prompt := fmt.Sprintf(`You are a senior security researcher synthesizing findings from a multi-agent security review.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s

## Debate Outputs from Security Review Agents

`, p.Overview, subsystem.Name, subsystem.Paths, subsystem.Description)

	for i, debate := range debates {
		prompt += fmt.Sprintf("### Debater %d's Analysis\n%s\n\n", i+1, debate)
	}

	prompt += `## Your Task

Review all the debate outputs above and synthesize them into a final security report. Focus on:

1. **Critical Findings** - Issues that are clearly exploitable and high-impact
2. **Likely Vulnerabilities** - Issues that appear exploitable given the codebase context
3. **Areas of Concern** - Patterns that warrant further investigation

For each finding, provide:
- A clear description of the vulnerability
- The specific location(s) in the code
- Potential impact and exploitability
- Recommended remediation

Prioritize quality over quantity. Only include findings that have strong evidence and are actionable.
Do NOT include theoretical issues or best-practice violations unless they represent real security risks.`

	return prompt
}
