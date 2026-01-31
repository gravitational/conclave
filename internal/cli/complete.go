package cli

import (
	"fmt"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/state"
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
	PreRunE: validateProvidersPreRun,
	RunE:    runComplete,
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

	display.PrintHeader("SYNTHESIZE")
	display.PrintStatus("Plan: %s", p.Name)
	display.PrintStatus("Subsystem: %s", completeSubsystem)

	// Load debates
	debates, err := st.LoadDebates(p.ID, completeSubsystem)
	if err != nil {
		return fmt.Errorf("failed to load debates: %w", err)
	}

	if len(debates) == 0 {
		return fmt.Errorf("no debates found - run 'conclave convene' first")
	}

	display.PrintStatus("Loaded %d debate outputs", len(debates))
	cfg := GetRuntimeConfig()
	if cfg != nil && cfg.IsConfigured() {
		display.PrintStatus("Provider: %s", cfg.PrimaryBackend())
	} else {
		display.PrintStatus("Provider: %s", PrimaryBackend())
	}
	fmt.Println()

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

	// Generate synthesis
	prompt := generateSynthesisPrompt(p, subsystem, debates)
	var ag agent.Agent
	if cfg != nil && cfg.IsConfigured() {
		ag = cfg.CompleteAgent()
	} else {
		ag = CreateAgent()
	}
	result := agent.StreamSilent(ag, prompt, "Synthesizing findings")

	// Save result
	path, err := st.SaveResult(p.ID, completeSubsystem, result)
	if err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	fmt.Println()
	display.PrintSuccess("Results saved: %s", path)

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

Review all the debate outputs above and synthesize them into a final security report with the following sections:

1. **Confirmed Vulnerabilities** - Issues the reviewers agreed are real and exploitable
   - Include severity, specific code locations, and exploitation details
   - For each finding, note which debaters identified it (e.g., "Found by: Debater 1, Debater 2")

2. **Disputed/Unclear** - Issues where debaters disagreed
   - Note the disagreement (e.g., "Debater 1 found this; Debater 3 disputed")
   - Include the reasoning from both sides

3. **Dismissed** - Issues determined to be false positives or non-exploitable
   - Note which debater(s) initially raised them and why they were dismissed

4. **Agent Comparison Summary**
   - Briefly summarize each debater's approach and key findings
   - Note any patterns in what each debater focused on
   - Highlight areas of agreement and disagreement

5. **Recommendations** - Prioritized remediation steps

Prioritize quality over quantity. Only include findings that have strong evidence and are actionable.
Do NOT include theoretical issues or best-practice violations unless they represent real security risks.`

	return prompt
}
