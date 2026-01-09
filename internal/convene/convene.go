package convene

import (
	"fmt"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/context"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

// DebateGenerator generates debate prompts
type DebateGenerator struct {
	agent   agent.Agent
	context *context.RepoContext
}

// NewDebateGenerator creates a new debate generator
func NewDebateGenerator(ag agent.Agent) *DebateGenerator {
	return &DebateGenerator{agent: ag}
}

// WithContext sets the repository context for prompts
func (g *DebateGenerator) WithContext(ctx *context.RepoContext) *DebateGenerator {
	g.context = ctx
	return g
}

// GeneratePrompts creates debate prompts that include all perspectives
func (g *DebateGenerator) GeneratePrompts(plan *state.Plan, subsystem string, perspectives []string) ([]string, error) {
	// Find subsystem details
	var sub *state.Subsystem
	for i := range plan.Subsystems {
		if plan.Subsystems[i].Slug == subsystem {
			sub = &plan.Subsystems[i]
			break
		}
	}

	if sub == nil {
		return nil, fmt.Errorf("subsystem not found: %s", subsystem)
	}

	// Build context with all perspectives
	perspectivesText := ""
	for i, p := range perspectives {
		perspectivesText += fmt.Sprintf("\n### Agent %d's Assessment\n%s\n", i+1, p)
	}

	// Build repository context section
	repoContextSection := ""
	if g.context != nil {
		if general := g.context.ForPrompt(); general != "" {
			repoContextSection += "\n" + general + "\n"
		}
		if specific := g.context.ForSubsystemPrompt(subsystem); specific != "" {
			repoContextSection += "\n" + specific + "\n"
		}
	}

	baseContext := fmt.Sprintf(`## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s
%s
## Security Assessments from Initial Review
%s
`, plan.Overview, sub.Name, sub.Paths, sub.Description, repoContextSection, perspectivesText)

	// Generate three debate prompts with different roles
	prompts := []string{
		fmt.Sprintf(`You are a security researcher participating in a peer review debate.

%s

## Your Role: The Skeptic

Your job is to critically examine the findings above. For each claimed vulnerability:
1. Challenge the exploitability - is this actually exploitable in practice?
2. Question the severity ratings - are they justified?
3. Identify any false positives or overblown concerns
4. Point out any missing context that would affect the assessment
5. BUT also acknowledge findings that ARE valid and serious

If you find the assessments generally sound, propose additional attack vectors they may have missed.

Be rigorous but fair. The goal is to refine the findings to the most actionable and serious issues.`, baseContext),

		fmt.Sprintf(`You are a security researcher participating in a peer review debate.

%s

## Your Role: The Advocate

Your job is to strengthen the case for the valid findings. For each vulnerability:
1. Provide additional evidence or attack scenarios
2. Explain the real-world impact more clearly
3. Connect vulnerabilities that could be chained together
4. Prioritize which issues need immediate attention
5. Dismiss findings that truly aren't exploitable

Also identify any security issues the other agents missed entirely.

Be constructive. The goal is to ensure the most serious issues are clearly communicated.`, baseContext),

		fmt.Sprintf(`You are a security researcher participating in a peer review debate.

%s

## Your Role: The Synthesizer

Your job is to find common ground and synthesize insights. You should:
1. Identify which findings multiple agents agreed on (these are likely valid)
2. Resolve disagreements by analyzing the actual code/context
3. Rank all findings by actual risk (considering both impact and exploitability)
4. Propose a clear remediation priority order
5. Call out any gaps in the overall security review

Be balanced. The goal is to produce a coherent, actionable list of security findings.`, baseContext),
	}

	return prompts, nil
}
