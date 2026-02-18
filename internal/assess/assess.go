package assess

import (
	"fmt"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/context"
	"github.com/rob-picard-teleport/conclave/internal/prompts"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

// PromptGenerator generates assessment prompts using an LLM
type PromptGenerator struct {
	agent             agent.Agent
	context           *context.RepoContext
	useDynamicPrompts bool // When true, uses LLM to generate prompts; false uses static prompts
}

// NewPromptGenerator creates a new prompt generator
func NewPromptGenerator(ag agent.Agent) *PromptGenerator {
	return &PromptGenerator{agent: ag}
}

// WithContext sets the repository context for prompts
func (g *PromptGenerator) WithContext(ctx *context.RepoContext) *PromptGenerator {
	g.context = ctx
	return g
}

// WithDynamicPrompts enables dynamic prompt generation using an LLM
// By default, static prompts are used for consistency and token savings
func (g *PromptGenerator) WithDynamicPrompts() *PromptGenerator {
	g.useDynamicPrompts = true
	return g
}

// GeneratePrompts creates unique prompts for assessing a subsystem
// Uses default count of 3 assessors
func (g *PromptGenerator) GeneratePrompts(plan *state.Plan, subsystem *state.Subsystem) ([]string, error) {
	return g.GeneratePromptsN(plan, subsystem, 3)
}

// GeneratePromptsN creates n unique prompts for assessing a subsystem
func (g *PromptGenerator) GeneratePromptsN(plan *state.Plan, subsystem *state.Subsystem, n int) ([]string, error) {
	// Cache context generation (optimization: generate once, use in both paths)
	var generalContext, subsystemContext string
	if g.context != nil {
		generalContext = g.context.ForPrompt(false) // Use full context for assessment stage
		subsystemContext = g.context.ForSubsystemPrompt(subsystem.Slug)
	}

	// Build context section for the meta-prompt
	contextInstructions := ""
	if generalContext != "" {
		contextInstructions = fmt.Sprintf(`
## Repository Context (from previous audits)
The following context has been learned from previous audits. Include relevant parts in each prompt:

%s

IMPORTANT: Each generated prompt MUST include instructions to:
- NOT report any known false positives listed above
- Pay special attention to the focus areas
- Skip any ignore patterns
- Not re-report previously confirmed findings unless their status has changed
`, generalContext)
	}

	// Use dynamic prompt generation only if explicitly enabled (opt-in)
	// Default: use static prompts for token savings and consistency
	if g.useDynamicPrompts {
		metaPrompt := prompts.Render(prompts.AssessDynamic, map[string]any{
			"N":                   n,
			"Overview":            plan.Overview,
			"Name":                subsystem.Name,
			"Paths":               subsystem.Paths,
			"Description":         subsystem.Description,
			"Interactions":        subsystem.Interactions,
			"ContextInstructions": contextInstructions,
		})

		output, err := agent.RunAndCollect(g.agent, metaPrompt)
		if err == nil {
			parsed := parsePromptsN(output, n)
			if len(parsed) >= n {
				return parsed[:n], nil
			}
		}
		// Fall through to static prompts if dynamic generation failed
	}

	// Use static prompts (default, or fallback from dynamic)
	return g.staticPromptsN(plan, subsystem, generalContext, subsystemContext, n), nil
}

func (g *PromptGenerator) staticPrompts(plan *state.Plan, subsystem *state.Subsystem, generalContext, subsystemContext string) []string {
	return g.staticPromptsN(plan, subsystem, generalContext, subsystemContext, 3)
}

func (g *PromptGenerator) staticPromptsN(plan *state.Plan, subsystem *state.Subsystem, generalContext, subsystemContext string, n int) []string {
	contextSection := ""
	if generalContext != "" {
		contextSection += "\n" + generalContext + "\n"
	}
	if subsystemContext != "" {
		contextSection += "\n" + subsystemContext + "\n"
	}

	prompt := prompts.Render(prompts.Assess, map[string]any{
		"Overview":       plan.Overview,
		"Name":           subsystem.Name,
		"Paths":          subsystem.Paths,
		"Description":    subsystem.Description,
		"Interactions":   subsystem.Interactions,
		"ContextSection": contextSection,
	})

	// All n agents get the same prompt - if they converge on the same issue, that's a strong signal
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = prompt
	}
	return result
}

func parsePrompts(output string) []string {
	return parsePromptsN(output, 3)
}

func parsePromptsN(output string, n int) []string {
	var parsed []string

	// Build dynamic markers for n prompts
	for i := 1; i <= n; i++ {
		start := fmt.Sprintf("---PROMPT%d---", i)
		var end string
		if i < n {
			end = fmt.Sprintf("---PROMPT%d---", i+1)
		} else {
			end = "---END---"
		}

		startIdx := indexOf(output, start)
		endIdx := indexOf(output, end)

		if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
			prompt := output[startIdx+len(start) : endIdx]
			parsed = append(parsed, trimWhitespace(prompt))
		}
	}

	return parsed
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func trimWhitespace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
