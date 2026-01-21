package convene

import (
	"fmt"
	"strings"

	"github.com/rob-picard-teleport/conclave/internal/context"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

// FilterValidFindings returns perspectives that contain actual findings
// (filters out "No critical vulnerabilities found" responses)
func FilterValidFindings(perspectives []state.Perspective) []state.Perspective {
	var valid []state.Perspective
	for _, p := range perspectives {
		lower := strings.ToLower(p.Content)
		if !strings.Contains(lower, "no critical vulnerabilities found") &&
			!strings.Contains(lower, "no significant vulnerabilities") &&
			!strings.Contains(lower, "no major vulnerabilities") {
			valid = append(valid, p)
		}
	}
	return valid
}

// Debate manages multi-round debates between agents
type Debate struct {
	context *context.RepoContext
	plan    *state.Plan
	sub     *state.Subsystem
}

// DebateRound captures agent output with metadata for debate rounds
type DebateRound struct {
	AgentNum int
	Provider string
	Model    string
	Content  string
}

// FormatLabel returns a human-readable label like "Agent 1 (Claude/sonnet)"
func (r *DebateRound) FormatLabel() string {
	if r.Model != "" {
		return fmt.Sprintf("Agent %d (%s/%s)", r.AgentNum, capitalize(r.Provider), r.Model)
	}
	if r.Provider != "" && r.Provider != "unknown" {
		return fmt.Sprintf("Agent %d (%s)", r.AgentNum, capitalize(r.Provider))
	}
	return fmt.Sprintf("Agent %d", r.AgentNum)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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

// SteelManPrompts creates prompts for the steel man phase (advocate for each finding)
// Returns one prompt per finding
func (d *Debate) SteelManPrompts(findings []state.Perspective) []string {
	prompts := make([]string, len(findings))

	for i, finding := range findings {
		prompts[i] = fmt.Sprintf(`You are an advocate for this security finding. Your job is to make the STRONGEST
possible case that this is a real, exploitable vulnerability.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s

## Finding from Security Researcher (%s)
%s

Build the strongest case:
1. Why this vulnerability is real and exploitable
2. Specific attack scenarios with step-by-step exploitation
3. What an attacker gains (concrete impact)
4. Why this should be prioritized for immediate fix

Be thorough and persuasive. Assume the finding is valid and argue for it.
`, d.plan.Overview, d.sub.Name, d.sub.Paths, d.sub.Description, finding.FormatLabel(), finding.Content)
	}

	return prompts
}

// CritiquePrompts creates prompts for the critique phase (argue against each finding)
// Returns one prompt per finding, each seeing the original finding + steel man argument
func (d *Debate) CritiquePrompts(findings []state.Perspective, steelMen []DebateRound) []string {
	prompts := make([]string, len(findings))

	for i := range findings {
		finding := findings[i]
		steelMan := steelMen[i]

		prompts[i] = fmt.Sprintf(`You are a skeptical security reviewer. Your job is to argue that this finding
should NOT be raised to engineers.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s

## Original Finding (%s)
%s

## Advocate's Argument (Steel Man)
%s

Argue against raising this finding:
1. Why it might be a false positive (misread code, wrong assumptions)
2. Why it's not exploitable in practice (mitigating factors, prerequisites)
3. Why the severity is overstated
4. Why busy engineers shouldn't spend time on this

Be rigorous. Find weaknesses in the argument. Challenge assumptions.
`, d.plan.Overview, d.sub.Name, d.sub.Paths, d.sub.Description, finding.FormatLabel(), finding.Content, steelMan.Content)
	}

	return prompts
}

// JudgePrompts creates prompts for the judge phase (decide RAISE or DISMISS)
// Returns one prompt per finding, each seeing finding + steel man + critique
func (d *Debate) JudgePrompts(findings []state.Perspective, steelMen, critiques []DebateRound) []string {
	prompts := make([]string, len(findings))

	for i := range findings {
		finding := findings[i]
		steelMan := steelMen[i]
		critique := critiques[i]

		prompts[i] = fmt.Sprintf(`You are an impartial judge deciding whether to raise this finding to engineers.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s

## Original Finding (%s)
%s

## Advocate's Argument (FOR raising)
%s

## Critic's Argument (AGAINST raising)
%s

Render your verdict in this EXACT format:

VERDICT: [RAISE or DISMISS]

REASONING:
[2-3 sentences explaining your decision, weighing both arguments]

CONFIDENCE: [HIGH/MEDIUM/LOW]

Be decisive. Engineers' time is valuable - only RAISE findings worth their attention.
`, d.plan.Overview, d.sub.Name, d.sub.Paths, d.sub.Description, finding.FormatLabel(), finding.Content, steelMan.Content, critique.Content)
	}

	return prompts
}

// SteelManPromptForFinding creates a steel man prompt for a single finding
func (d *Debate) SteelManPromptForFinding(finding state.Perspective) string {
	return fmt.Sprintf(`You are an advocate for this security finding. Your job is to make the STRONGEST
possible case that this is a real, exploitable vulnerability.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s

## Finding from Security Researcher (%s)
%s

Build the strongest case:
1. Why this vulnerability is real and exploitable
2. Specific attack scenarios with step-by-step exploitation
3. What an attacker gains (concrete impact)
4. Why this should be prioritized for immediate fix

Be thorough and persuasive. Assume the finding is valid and argue for it.
`, d.plan.Overview, d.sub.Name, d.sub.Paths, d.sub.Description, finding.FormatLabel(), finding.Content)
}

// CritiquePromptForFinding creates a critique prompt for a single finding with its steel man
func (d *Debate) CritiquePromptForFinding(finding state.Perspective, steelMan DebateRound) string {
	return fmt.Sprintf(`You are a skeptical security reviewer. Your job is to argue that this finding
should NOT be raised to engineers.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s

## Original Finding (%s)
%s

## Advocate's Argument (Steel Man)
%s

Argue against raising this finding:
1. Why it might be a false positive (misread code, wrong assumptions)
2. Why it's not exploitable in practice (mitigating factors, prerequisites)
3. Why the severity is overstated
4. Why busy engineers shouldn't spend time on this

Be rigorous. Find weaknesses in the argument. Challenge assumptions.
`, d.plan.Overview, d.sub.Name, d.sub.Paths, d.sub.Description, finding.FormatLabel(), finding.Content, steelMan.Content)
}

// JudgePromptForFinding creates a judge prompt for a single finding with steel man and critique
func (d *Debate) JudgePromptForFinding(finding state.Perspective, steelMan, critique DebateRound) string {
	return fmt.Sprintf(`You are an impartial judge deciding whether to raise this finding to engineers.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s

## Original Finding (%s)
%s

## Advocate's Argument (FOR raising)
%s

## Critic's Argument (AGAINST raising)
%s

Render your verdict in this EXACT format:

VERDICT: [RAISE or DISMISS]

REASONING:
[2-3 sentences explaining your decision, weighing both arguments]

CONFIDENCE: [HIGH/MEDIUM/LOW]

Be decisive. Engineers' time is valuable - only RAISE findings worth their attention.
`, d.plan.Overview, d.sub.Name, d.sub.Paths, d.sub.Description, finding.FormatLabel(), finding.Content, steelMan.Content, critique.Content)
}

// SynthesisPrompt creates the final synthesis prompt combining all verdicts
func (d *Debate) SynthesisPrompt(findings []state.Perspective, steelMen, critiques, judges []DebateRound) string {
	var b strings.Builder

	b.WriteString("## Findings and Verdicts\n\n")
	for i := range findings {
		finding := findings[i]
		judge := judges[i]

		b.WriteString(fmt.Sprintf("### Finding %d (%s)\n", i+1, finding.FormatLabel()))
		b.WriteString(fmt.Sprintf("**Original Finding:**\n%s\n\n", finding.Content))
		b.WriteString(fmt.Sprintf("**Judge's Verdict:**\n%s\n\n", judge.Content))
		b.WriteString("---\n\n")
	}

	return fmt.Sprintf(`You are producing the final security report after adversarial review.

## Codebase Context
%s

## Subsystem Under Review
**Name:** %s
**Paths:** %s
**Description:** %s

%s

Produce the final report:

1. **Confirmed Vulnerabilities** - Findings with RAISE verdict
   - Merge any convergent findings (multiple assessors found same issue)
   - Include: severity, location, exploitation details, remediation
   - Note which agent(s) originally identified each issue

2. **Dismissed** - Findings with DISMISS verdict
   - Brief note on why each was dismissed

3. **Recommendations** - Prioritized remediation steps

If multiple assessors identified the same vulnerability, synthesize into a single
finding with the most complete information across all reports.

Be definitive. This is the final report.
`, d.plan.Overview, d.sub.Name, d.sub.Paths, d.sub.Description, b.String())
}

