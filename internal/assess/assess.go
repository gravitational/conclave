package assess

import (
	"fmt"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/context"
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

// GeneratePrompts creates 3 unique prompts for assessing a subsystem
func (g *PromptGenerator) GeneratePrompts(plan *state.Plan, subsystem *state.Subsystem) ([]string, error) {
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
		// Generate prompts dynamically using the LLM
		metaPrompt := fmt.Sprintf(`You are generating prompts for a security review. Generate 3 different prompts that will be given to 3 separate security review agents.

Each prompt should instruct the agent to independently review a subsystem for security vulnerabilities.

## Codebase Context
%s

## Subsystem to Review
**Name:** %s
**Paths:** %s
**Description:** %s
**Interactions:** %s
%s
Generate 3 different prompts for independent security reviews. Each should encourage thorough exploration and finding serious, exploitable vulnerabilities with specific code locations and attack scenarios.

CRITICAL: In each generated prompt, you MUST include the ACTUAL subsystem details:
- Use the actual name "%s" (not a placeholder)
- Use the actual paths "%s" (not a placeholder)
- Use the actual description (not a placeholder)
Each prompt must contain the real subsystem information so the reviewing agent knows what to review.

Output format - use exactly this format with the markers:

---PROMPT1---
<first prompt here>
---PROMPT2---
<second prompt here>
---PROMPT3---
<third prompt here>
---END---
`, plan.Overview, subsystem.Name, subsystem.Paths, subsystem.Description, subsystem.Interactions, contextInstructions, subsystem.Name, subsystem.Paths)

		output, err := agent.RunAndCollect(g.agent, metaPrompt)
		if err == nil {
			prompts := parsePrompts(output)
			if len(prompts) >= 3 {
				return prompts[:3], nil
			}
		}
		// Fall through to static prompts if dynamic generation failed
	}

	// Use static prompts (default, or fallback from dynamic)
	return g.staticPrompts(plan, subsystem, generalContext, subsystemContext), nil
}

func (g *PromptGenerator) staticPrompts(plan *state.Plan, subsystem *state.Subsystem, generalContext, subsystemContext string) []string {
	// Build context section (using cached context passed as parameters)
	contextSection := ""
	if generalContext != "" {
		contextSection += "\n" + generalContext + "\n"
	}
	if subsystemContext != "" {
		contextSection += "\n" + subsystemContext + "\n"
	}

	prompt := fmt.Sprintf(`You are a senior security researcher conducting a thorough security review.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s
**Interactions:** %s
%s
## Your Task

Conduct an independent security review of this subsystem. Explore the code thoroughly and identify any security vulnerabilities you find.

For each finding:
1. Identify the specific vulnerable code location (file and line)
2. Explain the attack vector clearly
3. Assess real-world exploitability in this context
4. Rate severity (Critical/High/Medium)

Focus only on SERIOUS, EXPLOITABLE issues. Ignore theoretical concerns, best practice nitpicks, or low-impact issues.

Be thorough. Follow the code paths. Think like an attacker.
`, plan.Overview, subsystem.Name, subsystem.Paths, subsystem.Description, subsystem.Interactions, contextSection)

	// Vary the framing slightly
	return []string{
		prompt,
		prompt + "\nTake your time and be thorough.",
		prompt + "\nThink creatively about how this code could be abused.",
	}
}

func parsePrompts(output string) []string {
	var prompts []string

	markers := []struct{ start, end string }{
		{"---PROMPT1---", "---PROMPT2---"},
		{"---PROMPT2---", "---PROMPT3---"},
		{"---PROMPT3---", "---END---"},
	}

	for _, m := range markers {
		startIdx := indexOf(output, m.start)
		endIdx := indexOf(output, m.end)

		if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
			prompt := output[startIdx+len(m.start) : endIdx]
			prompts = append(prompts, trim(prompt))
		}
	}

	return prompts
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	// Trim whitespace
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
