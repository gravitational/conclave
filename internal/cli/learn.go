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

The LLM analyzes audit results and decides what's worth remembering
for future audits of this codebase.`,
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
	updates, err := parseLearnResponse(output)
	if err != nil {
		return fmt.Errorf("failed to parse learnings: %w", err)
	}

	// Update overview if not set
	if ctx.Overview == "" {
		ctx.SetOverview(plan.Overview)
	}

	applyLearnUpdates(ctx, updates)

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
<list patterns that are false positives, one per line>
---CONFIRMED_FINDINGS---
<list each on its own line, format: "subsystem: title | description | severity">
---NOTES---
<list each on its own line, format: "subsystem: note text">
---FOCUS_AREAS---
<list each on its own line>
---IGNORE_PATTERNS---
<list file patterns to ignore, one per line>
---END---
`, plan.Overview, strings.Join(results, "\n\n"), existingFindings)
}

type learnUpdates struct {
	FalsePositives    []struct{ Subsystem, Pattern, Reason string }
	ConfirmedFindings []struct{ Subsystem, Title, Description, Status string }
	Notes             []struct{ Subsystem, Note string }
	FocusAreas        []string
	IgnorePatterns    []string
}

func parseLearnResponse(output string) (*learnUpdates, error) {
	updates := &learnUpdates{}

	sections := map[string]bool{
		"---FALSE_POSITIVES---":    true,
		"---CONFIRMED_FINDINGS---": true,
		"---NOTES---":              true,
		"---FOCUS_AREAS---":        true,
		"---IGNORE_PATTERNS---":    true,
	}

	currentSection := ""
	var currentLines []string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if sections[line] {
			if currentSection != "" {
				processLearnSection(updates, currentSection, currentLines)
			}
			currentSection = line
			currentLines = nil
			continue
		}

		if line == "---END---" {
			if currentSection != "" {
				processLearnSection(updates, currentSection, currentLines)
			}
			break
		}

		if currentSection != "" {
			currentLines = append(currentLines, line)
		}
	}

	return updates, nil
}

func processLearnSection(updates *learnUpdates, section string, lines []string) {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "<") {
			continue
		}

		switch section {
		case "---FALSE_POSITIVES---":
			if parts := splitLearnLine(line); len(parts) >= 2 {
				updates.FalsePositives = append(updates.FalsePositives, struct{ Subsystem, Pattern, Reason string }{
					Subsystem: parts[0],
					Pattern:   parts[1],
					Reason:    safeGetPart(parts, 2),
				})
			}

		case "---CONFIRMED_FINDINGS---":
			if parts := splitLearnLine(line); len(parts) >= 2 {
				status := safeGetPart(parts, 3)
				if status == "" {
					status = "confirmed"
				}
				updates.ConfirmedFindings = append(updates.ConfirmedFindings, struct{ Subsystem, Title, Description, Status string }{
					Subsystem:   parts[0],
					Title:       parts[1],
					Description: safeGetPart(parts, 2),
					Status:      status,
				})
			}

		case "---NOTES---":
			if idx := strings.Index(line, ":"); idx > 0 {
				updates.Notes = append(updates.Notes, struct{ Subsystem, Note string }{
					Subsystem: strings.TrimSpace(line[:idx]),
					Note:      strings.TrimSpace(line[idx+1:]),
				})
			}

		case "---FOCUS_AREAS---":
			updates.FocusAreas = append(updates.FocusAreas, line)

		case "---IGNORE_PATTERNS---":
			updates.IgnorePatterns = append(updates.IgnorePatterns, line)
		}
	}
}

func splitLearnLine(line string) []string {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return nil
	}

	subsystem := strings.TrimSpace(line[:idx])
	rest := strings.TrimSpace(line[idx+1:])

	parts := strings.Split(rest, "|")
	result := []string{subsystem}
	for _, p := range parts {
		result = append(result, strings.TrimSpace(p))
	}
	return result
}

func safeGetPart(parts []string, idx int) string {
	if idx < len(parts) {
		return parts[idx]
	}
	return ""
}

func applyLearnUpdates(ctx *context.RepoContext, updates *learnUpdates) {
	for _, fp := range updates.FalsePositives {
		ctx.AddFalsePositive(fp.Subsystem, fp.Pattern, fp.Reason)
	}

	for _, f := range updates.ConfirmedFindings {
		ctx.AddFinding(f.Subsystem, f.Title, f.Description, f.Status)
	}

	for _, n := range updates.Notes {
		ctx.AddSubsystemNote(n.Subsystem, n.Note)
	}

	for _, area := range updates.FocusAreas {
		ctx.AddFocusArea(area)
	}

	for _, pattern := range updates.IgnorePatterns {
		ctx.AddIgnorePattern(pattern)
	}
}
