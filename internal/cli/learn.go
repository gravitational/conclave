package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/context"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/spf13/cobra"
)

var learnPlanID string

var learnCmd = &cobra.Command{
	Use:   "learn",
	Short: "Automatically extract learnings from audit results",
	Long: `Review the latest audit results and automatically update CONCLAVE.md
with key findings, patterns, and context for future audits.

This is like 'feedback' but automated - the LLM decides what's worth
remembering based on the audit results.`,
	Args:    cobra.NoArgs,
	PreRunE: validateProvidersPreRun,
	RunE:    runLearn,
}

func init() {
	learnCmd.Flags().StringVar(&learnPlanID, "plan", "", "Plan ID to learn from (default: most recent)")
	rootCmd.AddCommand(learnCmd)
}

func runLearn(cmd *cobra.Command, args []string) error {
	absPath, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Load existing context
	ctx, err := context.Load(absPath)
	if err != nil {
		return fmt.Errorf("failed to load context: %w", err)
	}

	// Load state
	st, err := state.New(absPath)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	// Load plan
	var plan *state.Plan
	if learnPlanID != "" {
		plan, err = st.LoadPlanByID(learnPlanID)
		if err != nil {
			return fmt.Errorf("plan not found: %s", learnPlanID)
		}
	} else {
		plan, err = st.LoadMostRecentPlan()
		if err != nil {
			return fmt.Errorf("no plans found - run 'conclave run' first")
		}
	}

	display.PrintHeader("LEARNING FROM AUDIT")
	display.PrintStatus("Plan: %s (%s)", plan.Name, plan.ID[:8])

	// Collect all results
	var results []string
	var assessedSubs []string
	for _, sub := range plan.Subsystems {
		result, err := st.LoadResult(plan.ID, sub.Slug)
		if err == nil && result != "" {
			results = append(results, fmt.Sprintf("## %s\n%s", sub.Name, result))
			assessedSubs = append(assessedSubs, sub.Name)
		}
	}

	if len(results) == 0 {
		return fmt.Errorf("no results found for this plan")
	}

	display.PrintStatus("Results: %s", strings.Join(assessedSubs, ", "))
	fmt.Println()

	// Build prompt for extraction
	prompt := buildLearnPrompt(plan, results, ctx)

	ag := CreateAgent()
	output := agent.StreamSilent(ag, prompt, "Extracting learnings")

	// Parse and apply
	updates, err := parseFeedbackResponse(output)
	if err != nil {
		return fmt.Errorf("failed to parse learnings: %w", err)
	}

	// Update overview if not set
	if ctx.Overview == "" {
		ctx.SetOverview(plan.Overview)
	}

	applyUpdates(ctx, updates)

	if err := ctx.Save(); err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}

	fmt.Println()
	display.PrintSuccess("Context updated: %s", ctx.Path())

	total := len(updates.FalsePositives) + len(updates.ConfirmedFindings) +
		len(updates.Notes) + len(updates.FocusAreas) + len(updates.IgnorePatterns)

	if total == 0 {
		display.PrintStatus("No new learnings extracted")
	} else {
		if len(updates.ConfirmedFindings) > 0 {
			display.PrintStatus("Recorded %d finding(s)", len(updates.ConfirmedFindings))
		}
		if len(updates.Notes) > 0 {
			display.PrintStatus("Added %d note(s)", len(updates.Notes))
		}
		if len(updates.FocusAreas) > 0 {
			display.PrintStatus("Added %d focus area(s)", len(updates.FocusAreas))
		}
	}

	return nil
}

func buildLearnPrompt(plan *state.Plan, results []string, ctx *context.RepoContext) string {
	existingFindings := ""
	if len(ctx.Findings) > 0 {
		existingFindings = "\nAlready recorded findings (don't duplicate):\n"
		for _, f := range ctx.Findings {
			existingFindings += fmt.Sprintf("- %s: %s\n", f.Subsystem, f.Title)
		}
	}

	return fmt.Sprintf(`You are reviewing security audit results to extract key learnings for future audits.

## Codebase
%s

## Audit Results
%s
%s
Based on these results, extract:
1. CONFIRMED FINDINGS - Real security issues found (with severity and status)
2. NOTES - Important context about subsystems that would help future audits
3. FOCUS AREAS - Areas that deserve more attention in future audits

Do NOT extract:
- Theoretical or low-severity issues
- Things that are clearly false positives
- Duplicate findings already recorded

Output ONLY in this exact format:

---FALSE_POSITIVES---
<leave empty - user provides these via feedback>
---CONFIRMED_FINDINGS---
<list each on its own line, format: "subsystem: title | description | severity">
---NOTES---
<list each on its own line, format: "subsystem: note text">
---FOCUS_AREAS---
<list each on its own line>
---IGNORE_PATTERNS---
<leave empty - user provides these via feedback>
---END---
`, plan.Overview, strings.Join(results, "\n\n"), existingFindings)
}
