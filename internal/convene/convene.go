package convene

import (
	"fmt"
	"strings"

	"github.com/rob-picard-teleport/conclave/internal/context"
	"github.com/rob-picard-teleport/conclave/internal/prompts"
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
	result := make([]string, len(findings))
	for i, finding := range findings {
		result[i] = d.SteelManPromptForFinding(finding)
	}
	return result
}

// CritiquePrompts creates prompts for the critique phase (argue against each finding)
// Returns one prompt per finding, each seeing the original finding + steel man argument
func (d *Debate) CritiquePrompts(findings []state.Perspective, steelMen []DebateRound) []string {
	result := make([]string, len(findings))
	for i := range findings {
		result[i] = d.CritiquePromptForFinding(findings[i], steelMen[i])
	}
	return result
}

// JudgePrompts creates prompts for the judge phase (decide RAISE or DISMISS)
// Returns one prompt per finding, each seeing finding + steel man + critique
func (d *Debate) JudgePrompts(findings []state.Perspective, steelMen, critiques []DebateRound) []string {
	result := make([]string, len(findings))
	for i := range findings {
		result[i] = d.JudgePromptForFinding(findings[i], steelMen[i], critiques[i])
	}
	return result
}

// SteelManPromptForFinding creates a steel man prompt for a single finding
func (d *Debate) SteelManPromptForFinding(finding state.Perspective) string {
	return prompts.Render(prompts.ConveneSteelMan, map[string]any{
		"Overview":     d.plan.Overview,
		"Name":         d.sub.Name,
		"Paths":        d.sub.Paths,
		"Description":  d.sub.Description,
		"FindingLabel": finding.FormatLabel(),
		"Finding":      finding.Content,
	})
}

// CritiquePromptForFinding creates a critique prompt for a single finding with its steel man
func (d *Debate) CritiquePromptForFinding(finding state.Perspective, steelMan DebateRound) string {
	return prompts.Render(prompts.ConveneCritique, map[string]any{
		"Overview":     d.plan.Overview,
		"Name":         d.sub.Name,
		"Paths":        d.sub.Paths,
		"Description":  d.sub.Description,
		"FindingLabel": finding.FormatLabel(),
		"Finding":      finding.Content,
		"SteelMan":     steelMan.Content,
	})
}

// JudgePromptForFinding creates a judge prompt for a single finding with steel man and critique
func (d *Debate) JudgePromptForFinding(finding state.Perspective, steelMan, critique DebateRound) string {
	return prompts.Render(prompts.ConveneJudge, map[string]any{
		"Overview":     d.plan.Overview,
		"Name":         d.sub.Name,
		"Paths":        d.sub.Paths,
		"Description":  d.sub.Description,
		"FindingLabel": finding.FormatLabel(),
		"Finding":      finding.Content,
		"SteelMan":     steelMan.Content,
		"Critique":     critique.Content,
	})
}

// SynthesisPrompt creates the final synthesis prompt combining all verdicts
func (d *Debate) SynthesisPrompt(findings []state.Perspective, steelMen, critiques, judges []DebateRound) string {
	var b strings.Builder
	b.WriteString("## Findings and Verdicts\n\n")
	for i := range findings {
		b.WriteString(fmt.Sprintf("### Finding %d (%s)\n", i+1, findings[i].FormatLabel()))
		b.WriteString(fmt.Sprintf("**Original Finding:**\n%s\n\n", findings[i].Content))
		b.WriteString(fmt.Sprintf("**Judge's Verdict:**\n%s\n\n", judges[i].Content))
		b.WriteString("---\n\n")
	}

	return prompts.Render(prompts.ConveneSynthesis, map[string]any{
		"Overview":           d.plan.Overview,
		"Name":               d.sub.Name,
		"Paths":              d.sub.Paths,
		"Description":        d.sub.Description,
		"FindingsAndVerdicts": b.String(),
	})
}
