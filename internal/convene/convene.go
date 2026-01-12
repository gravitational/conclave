package convene

import (
	"fmt"
	"strings"

	"github.com/rob-picard-teleport/conclave/internal/context"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

// Debate manages multi-round debates between agents
type Debate struct {
	context *context.RepoContext
	plan    *state.Plan
	sub     *state.Subsystem
}

// NewDebate creates a new debate for a subsystem
func NewDebate(plan *state.Plan, subsystem string) (*Debate, error) {
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

	return &Debate{plan: plan, sub: sub}, nil
}

// WithContext sets the repository context
func (d *Debate) WithContext(ctx *context.RepoContext) *Debate {
	d.context = ctx
	return d
}

// Round1Prompts creates prompts for the first debate round (review initial findings)
func (d *Debate) Round1Prompts(perspectives []string) []string {
	base := d.buildBaseContext(perspectives, nil)

	prompt := fmt.Sprintf(`You are a security researcher reviewing initial findings from other researchers.

%s

Review these findings critically:
- Which findings are valid and exploitable?
- Which seem like false positives or overblown?
- What did other reviewers miss?
- How would you prioritize these issues?

Be specific. Reference the actual code and explain your reasoning.
`, base)

	// All agents get same prompt, vary slightly
	return []string{
		prompt,
		prompt + "\nBe thorough in your analysis.",
		prompt + "\nConsider how these vulnerabilities could be chained together.",
	}
}

// Round2Prompts creates prompts for the second round (respond to each other)
// Note: Does not repeat base context from Round 1 to save tokens
func (d *Debate) Round2Prompts(perspectives []string, round1 []string) []string {
	var b strings.Builder

	// Only include the Round 1 discussion, not the full base context
	// (agents already saw context in Round 1)
	b.WriteString("## Round 1 Discussion\n")
	for i, r := range round1 {
		b.WriteString(fmt.Sprintf("### Reviewer %d\n%s\n\n", i+1, r))
	}

	prompt := fmt.Sprintf(`You are a security researcher in a peer review discussion.

Refer to the codebase context and initial security assessments from Round 1.

%s

The debate has begun. Other researchers have shared their views above. Now:
- Respond to points you disagree with
- Reinforce points you agree with
- Resolve any conflicting assessments
- Refine the severity ratings based on the discussion

The goal is to converge on the true security issues.
`, b.String())

	return []string{
		prompt,
		prompt + "\nFocus on reaching consensus.",
		prompt + "\nHighlight the most critical issues that need immediate attention.",
	}
}

// FinalPrompt creates the synthesis prompt (final round, single agent)
// Note: Does not repeat base context from previous rounds to save tokens
func (d *Debate) FinalPrompt(perspectives []string, round1, round2 []string) string {
	// Combine all rounds (without repeating base context)
	var allDiscussion strings.Builder
	allDiscussion.WriteString("## Initial Assessments\n")
	for i, p := range perspectives {
		allDiscussion.WriteString(fmt.Sprintf("### Assessor %d\n%s\n\n", i+1, p))
	}
	allDiscussion.WriteString("## Debate Round 1\n")
	for i, r := range round1 {
		allDiscussion.WriteString(fmt.Sprintf("### Reviewer %d\n%s\n\n", i+1, r))
	}
	allDiscussion.WriteString("## Debate Round 2\n")
	for i, r := range round2 {
		allDiscussion.WriteString(fmt.Sprintf("### Reviewer %d\n%s\n\n", i+1, r))
	}

	return fmt.Sprintf(`You are producing the final security report after a thorough peer review.

Refer to the codebase context and subsystem details provided in previous rounds.

## Full Discussion
%s

Based on this complete discussion, produce the FINAL security report:

1. **Confirmed Vulnerabilities** - Issues the reviewers agreed are real and exploitable
   - Include severity, specific code locations, and exploitation details

2. **Disputed/Unclear** - Issues where reviewers disagreed (note the disagreement)

3. **Dismissed** - Issues determined to be false positives or non-exploitable

4. **Recommendations** - Prioritized remediation steps

Be definitive. This is the final report.
`, allDiscussion.String())
}

func (d *Debate) buildBaseContext(perspectives []string, priorRound []string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf(`## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s
`, d.plan.Overview, d.sub.Name, d.sub.Paths, d.sub.Description))

	if d.context != nil {
		if general := d.context.ForPrompt(false); general != "" { // Use full context for Round 1
			b.WriteString("\n" + general + "\n")
		}
		if specific := d.context.ForSubsystemPrompt(d.sub.Slug); specific != "" {
			b.WriteString("\n" + specific + "\n")
		}
	}

	b.WriteString("\n## Initial Security Assessments\n")
	for i, p := range perspectives {
		b.WriteString(fmt.Sprintf("### Assessor %d\n%s\n\n", i+1, p))
	}

	if len(priorRound) > 0 {
		b.WriteString("## Previous Round Discussion\n")
		for i, r := range priorRound {
			b.WriteString(fmt.Sprintf("### Reviewer %d\n%s\n\n", i+1, r))
		}
	}

	return b.String()
}
