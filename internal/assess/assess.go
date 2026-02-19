package assess

import (
	"github.com/rob-picard-teleport/conclave/internal/context"
	"github.com/rob-picard-teleport/conclave/internal/prompts"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

// PromptGenerator generates assessment prompts
type PromptGenerator struct {
	context *context.RepoContext
}

// NewPromptGenerator creates a new prompt generator
func NewPromptGenerator() *PromptGenerator {
	return &PromptGenerator{}
}

// WithContext sets the repository context for prompts
func (g *PromptGenerator) WithContext(ctx *context.RepoContext) *PromptGenerator {
	g.context = ctx
	return g
}

// GeneratePrompts creates prompts for assessing a subsystem
// Uses default count of 3 assessors
func (g *PromptGenerator) GeneratePrompts(plan *state.Plan, subsystem *state.Subsystem) ([]string, error) {
	return g.GeneratePromptsN(plan, subsystem, 3)
}

// GeneratePromptsN creates n prompts for assessing a subsystem
func (g *PromptGenerator) GeneratePromptsN(plan *state.Plan, subsystem *state.Subsystem, n int) ([]string, error) {
	var generalContext, subsystemContext string
	if g.context != nil {
		generalContext = g.context.ForPrompt(false)
		subsystemContext = g.context.ForSubsystemPrompt(subsystem.Slug)
	}

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
	return result, nil
}
