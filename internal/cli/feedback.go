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

var feedbackCmd = &cobra.Command{
	Use:   "feedback '<your feedback>'",
	Short: "Provide natural language feedback on findings",
	Long: `Record feedback about audit findings using natural language.

The feedback will be parsed by an LLM and used to update the CONCLAVE.md
context file, which improves future audits.

Examples:
  conclave feedback 'all false positives except the SQL injection, that one is real'

  conclave feedback 'the auth subsystem findings are all test code, ignore those paths'

  conclave feedback 'IDOR in user profile is confirmed and being fixed'

  conclave feedback 'focus more on the payment processing code next time'`,
	Args: cobra.ExactArgs(1),
	RunE: runFeedback,
}

func init() {
	rootCmd.AddCommand(feedbackCmd)
}

func runFeedback(cmd *cobra.Command, args []string) error {
	feedback := args[0]

	// Find codebase path (current directory)
	absPath, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Load existing context
	ctx, err := context.Load(absPath)
	if err != nil {
		return fmt.Errorf("failed to load context: %w", err)
	}

	// Load state to get recent findings
	st, err := state.New(absPath)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	// Get most recent plan and results
	plan, _ := st.LoadMostRecentPlan()

	// Build context about what was found
	findingsContext := buildFindingsContext(st, plan)

	display.PrintHeader("PROCESSING FEEDBACK")
	display.PrintStatus("Feedback: %s", feedback)
	fmt.Println()

	// Use LLM to parse the feedback
	ag := CreateAgent()
	prompt := buildFeedbackPrompt(feedback, findingsContext, ctx)

	output := agent.StreamSilent(ag, prompt, "Analyzing feedback")

	// Parse the LLM response and update context
	updates, err := parseFeedbackResponse(output)
	if err != nil {
		return fmt.Errorf("failed to parse feedback: %w", err)
	}

	// Apply updates
	applyUpdates(ctx, updates)

	// Save
	if err := ctx.Save(); err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}

	fmt.Println()
	display.PrintSuccess("Context updated: %s", ctx.Path())

	// Show what was updated
	if len(updates.FalsePositives) > 0 {
		display.PrintStatus("Added %d false positive(s)", len(updates.FalsePositives))
	}
	if len(updates.ConfirmedFindings) > 0 {
		display.PrintStatus("Added %d confirmed finding(s)", len(updates.ConfirmedFindings))
	}
	if len(updates.Notes) > 0 {
		display.PrintStatus("Added %d note(s)", len(updates.Notes))
	}
	if len(updates.FocusAreas) > 0 {
		display.PrintStatus("Added %d focus area(s)", len(updates.FocusAreas))
	}
	if len(updates.IgnorePatterns) > 0 {
		display.PrintStatus("Added %d ignore pattern(s)", len(updates.IgnorePatterns))
	}

	return nil
}

func buildFindingsContext(st *state.State, plan *state.Plan) string {
	if plan == nil {
		return "No recent audit findings available."
	}

	var b strings.Builder
	b.WriteString("Recent audit findings:\n\n")

	// List subsystems
	b.WriteString("Subsystems analyzed:\n")
	for _, sub := range plan.Subsystems {
		b.WriteString(fmt.Sprintf("- %s (%s)\n", sub.Name, sub.Slug))
	}
	b.WriteString("\n")

	// Try to load recent results
	for _, sub := range plan.Subsystems {
		result, err := st.LoadResult(plan.ID, sub.Slug)
		if err == nil && result != "" {
			b.WriteString(fmt.Sprintf("## Findings for %s:\n%s\n\n", sub.Name, truncate(result, 2000)))
		}
	}

	return b.String()
}

func buildFeedbackPrompt(feedback, findingsContext string, ctx *context.RepoContext) string {
	existingContext := ""
	if !ctx.Exists() {
		existingContext = "No existing context file."
	} else {
		existingContext = fmt.Sprintf(`Existing context:
- False positives: %d recorded
- Confirmed findings: %d recorded
- Focus areas: %d recorded
- Ignore patterns: %d recorded
- Subsystem notes: %d recorded`,
			len(ctx.FalsePositives),
			len(ctx.Findings),
			len(ctx.FocusAreas),
			len(ctx.IgnorePatterns),
			len(ctx.SubsystemNotes))
	}

	return fmt.Sprintf(`You are processing user feedback about security audit findings. Parse the feedback and extract structured information.

%s

%s

User feedback:
"%s"

Parse this feedback and output ONLY a structured response in this exact format:

---FALSE_POSITIVES---
<list each false positive on its own line, format: "subsystem: pattern | reason">
<leave empty if none mentioned>
---CONFIRMED_FINDINGS---
<list each confirmed finding on its own line, format: "subsystem: title | description | status">
<status should be: confirmed, fixing, fixed, or wontfix>
<leave empty if none mentioned>
---NOTES---
<list each note on its own line, format: "subsystem: note text">
<leave empty if none mentioned>
---FOCUS_AREAS---
<list each focus area on its own line>
<leave empty if none mentioned>
---IGNORE_PATTERNS---
<list each ignore pattern on its own line>
<leave empty if none mentioned>
---END---

Rules:
- Only include items explicitly mentioned or clearly implied by the user
- If user says "all false positives except X", list each false positive individually (excluding X)
- If user confirms a finding, put it in CONFIRMED_FINDINGS with appropriate status
- Use "*" as subsystem if the feedback applies globally
- Be concise but capture the user's intent accurately
`, findingsContext, existingContext, feedback)
}

type feedbackUpdates struct {
	FalsePositives    []struct{ Subsystem, Pattern, Reason string }
	ConfirmedFindings []struct{ Subsystem, Title, Description, Status string }
	Notes             []struct{ Subsystem, Note string }
	FocusAreas        []string
	IgnorePatterns    []string
}

func parseFeedbackResponse(output string) (*feedbackUpdates, error) {
	updates := &feedbackUpdates{}

	sections := map[string]*[]string{
		"---FALSE_POSITIVES---":    nil,
		"---CONFIRMED_FINDINGS---": nil,
		"---NOTES---":              nil,
		"---FOCUS_AREAS---":        nil,
		"---IGNORE_PATTERNS---":    nil,
	}

	// Find each section
	currentSection := ""
	var currentLines []string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if _, isSection := sections[line]; isSection {
			// Save previous section
			if currentSection != "" {
				processSectionLines(updates, currentSection, currentLines)
			}
			currentSection = line
			currentLines = nil
			continue
		}

		if line == "---END---" {
			if currentSection != "" {
				processSectionLines(updates, currentSection, currentLines)
			}
			break
		}

		if currentSection != "" {
			currentLines = append(currentLines, line)
		}
	}

	return updates, nil
}

func processSectionLines(updates *feedbackUpdates, section string, lines []string) {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "<") {
			continue
		}

		switch section {
		case "---FALSE_POSITIVES---":
			// Format: "subsystem: pattern | reason"
			if parts := splitFeedbackLine(line); len(parts) >= 2 {
				updates.FalsePositives = append(updates.FalsePositives, struct{ Subsystem, Pattern, Reason string }{
					Subsystem: parts[0],
					Pattern:   parts[1],
					Reason:    safeGet(parts, 2),
				})
			}

		case "---CONFIRMED_FINDINGS---":
			// Format: "subsystem: title | description | status"
			if parts := splitFeedbackLine(line); len(parts) >= 2 {
				status := safeGet(parts, 3)
				if status == "" {
					status = "confirmed"
				}
				updates.ConfirmedFindings = append(updates.ConfirmedFindings, struct{ Subsystem, Title, Description, Status string }{
					Subsystem:   parts[0],
					Title:       parts[1],
					Description: safeGet(parts, 2),
					Status:      status,
				})
			}

		case "---NOTES---":
			// Format: "subsystem: note text"
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

func splitFeedbackLine(line string) []string {
	// First split on ":" to get subsystem
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return nil
	}

	subsystem := strings.TrimSpace(line[:idx])
	rest := strings.TrimSpace(line[idx+1:])

	// Then split rest on "|"
	parts := strings.Split(rest, "|")
	result := []string{subsystem}
	for _, p := range parts {
		result = append(result, strings.TrimSpace(p))
	}
	return result
}

func safeGet(parts []string, idx int) string {
	if idx < len(parts) {
		return parts[idx]
	}
	return ""
}

func applyUpdates(ctx *context.RepoContext, updates *feedbackUpdates) {
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}
