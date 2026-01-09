package assess

import (
	"fmt"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

// PromptGenerator generates assessment prompts using an LLM
type PromptGenerator struct {
	agent agent.Agent
}

// NewPromptGenerator creates a new prompt generator
func NewPromptGenerator(ag agent.Agent) *PromptGenerator {
	return &PromptGenerator{agent: ag}
}

// GeneratePrompts creates 3 unique prompts for assessing a subsystem
func (g *PromptGenerator) GeneratePrompts(plan *state.Plan, subsystem *state.Subsystem) ([]string, error) {
	// Generate prompts dynamically using the LLM
	metaPrompt := fmt.Sprintf(`You are generating prompts for a security review. Generate 3 different prompts that will be given to 3 separate security review agents.

Each prompt should instruct the agent to review the following subsystem for security vulnerabilities, but from a different angle:

## Codebase Context
%s

## Subsystem to Review
**Name:** %s
**Paths:** %s
**Description:** %s
**Interactions:** %s

Generate exactly 3 prompts, each taking a different approach:
1. First prompt: Focus on input validation, injection attacks, and data handling vulnerabilities
2. Second prompt: Focus on authentication, authorization, access control, and privilege escalation
3. Third prompt: Focus on cryptography, data exposure, configuration issues, and logic flaws

Each prompt should:
- Tell the agent to focus only on SERIOUS, EXPLOITABLE vulnerabilities given the context
- Instruct them to explore the relevant code paths thoroughly
- Ask for specific file locations and code snippets
- Request clear exploitation scenarios

Output format - use exactly this format with the markers:

---PROMPT1---
<first prompt here>
---PROMPT2---
<second prompt here>
---PROMPT3---
<third prompt here>
---END---
`, plan.Overview, subsystem.Name, subsystem.Paths, subsystem.Description, subsystem.Interactions)

	output, err := agent.RunAndCollect(g.agent, metaPrompt)
	if err != nil {
		// Fall back to static prompts
		return g.staticPrompts(plan, subsystem), nil
	}

	prompts := parsePrompts(output)
	if len(prompts) < 3 {
		// Fall back to static prompts
		return g.staticPrompts(plan, subsystem), nil
	}

	return prompts[:3], nil
}

func (g *PromptGenerator) staticPrompts(plan *state.Plan, subsystem *state.Subsystem) []string {
	base := fmt.Sprintf(`You are a senior security researcher conducting a thorough security review.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s
**Interactions:** %s

`, plan.Overview, subsystem.Name, subsystem.Paths, subsystem.Description, subsystem.Interactions)

	prompts := []string{
		base + `## Your Focus: Input Validation & Injection Attacks

Review this subsystem for:
- SQL injection vulnerabilities
- Command injection vulnerabilities
- Path traversal vulnerabilities
- XSS (Cross-Site Scripting) if applicable
- Template injection
- Deserialization vulnerabilities
- Any other input validation issues

For each finding:
1. Identify the specific vulnerable code location
2. Explain the attack vector
3. Assess exploitability in context
4. Rate severity (Critical/High/Medium)

Focus only on SERIOUS, EXPLOITABLE issues. Ignore theoretical or low-impact concerns.`,

		base + `## Your Focus: Authentication, Authorization & Access Control

Review this subsystem for:
- Authentication bypass vulnerabilities
- Authorization flaws (IDOR, broken access control)
- Privilege escalation paths
- Session management issues
- JWT/token vulnerabilities
- API key exposure or mishandling
- Race conditions in auth flows

For each finding:
1. Identify the specific vulnerable code location
2. Explain the attack vector
3. Assess exploitability in context
4. Rate severity (Critical/High/Medium)

Focus only on SERIOUS, EXPLOITABLE issues. Ignore theoretical or low-impact concerns.`,

		base + `## Your Focus: Cryptography, Data Exposure & Logic Flaws

Review this subsystem for:
- Weak or broken cryptography
- Sensitive data exposure
- Information leakage
- Insecure configurations
- Logic flaws that could be exploited
- SSRF (Server-Side Request Forgery)
- Business logic vulnerabilities
- Error handling issues that leak information

For each finding:
1. Identify the specific vulnerable code location
2. Explain the attack vector
3. Assess exploitability in context
4. Rate severity (Critical/High/Medium)

Focus only on SERIOUS, EXPLOITABLE issues. Ignore theoretical or low-impact concerns.`,
	}

	return prompts
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
