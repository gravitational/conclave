package assess

import (
	"github.com/rob-picard-teleport/conclave/internal/prompts"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

// PromptGenerator generates assessment prompts
type PromptGenerator struct {
	instructions []string
}

// NewPromptGenerator creates a new prompt generator
func NewPromptGenerator() *PromptGenerator {
	return &PromptGenerator{}
}

// WithInstructions sets custom instructions from config
func (g *PromptGenerator) WithInstructions(instructions []string) *PromptGenerator {
	g.instructions = instructions
	return g
}

// GeneratePrompts creates prompts for assessing a subsystem
// Uses default count of 3 assessors
func (g *PromptGenerator) GeneratePrompts(plan *state.Plan, subsystem *state.Subsystem) ([]string, error) {
	return g.GeneratePromptsN(plan, subsystem, 3)
}

// GeneratePromptsN creates n prompts for assessing a subsystem
func (g *PromptGenerator) GeneratePromptsN(plan *state.Plan, subsystem *state.Subsystem, n int) ([]string, error) {
	contextSection := ""
	if len(g.instructions) > 0 {
		contextSection += "\n=== CUSTOM INSTRUCTIONS (follow these directives) ===\n"
		for _, inst := range g.instructions {
			contextSection += "- " + inst + "\n"
		}
		contextSection += "=== END CUSTOM INSTRUCTIONS ===\n"
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
