package plan

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/prompts"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

// Generator creates analysis plans
type Generator struct {
	agent agent.Agent
	state *state.State
}

// NewGenerator creates a new plan generator
func NewGenerator(ag agent.Agent, st *state.State) *Generator {
	return &Generator{
		agent: ag,
		state: st,
	}
}

// BuildPrompt returns the prompt for plan generation
func (g *Generator) BuildPrompt(codebasePath string) string {
	return prompts.Plan
}

// BuildRefinePrompt returns the prompt for refining an existing plan
func (g *Generator) BuildRefinePrompt(existingPlan *state.Plan) string {
	var sb strings.Builder
	for _, sub := range existingPlan.Subsystems {
		sb.WriteString(fmt.Sprintf("\n- %s (%s)\n", sub.Name, sub.Slug))
		sb.WriteString(fmt.Sprintf("  Paths: %s\n", sub.Paths))
		sb.WriteString(fmt.Sprintf("  Description: %s\n", sub.Description))
	}

	return prompts.Render(prompts.PlanRefine, map[string]any{
		"Name":            existingPlan.Name,
		"Overview":        existingPlan.Overview,
		"SubsystemsList":  sb.String(),
	})
}

// ParseAndSave parses agent output and saves the plan
func (g *Generator) ParseAndSave(output string, codebasePath string) (*state.Plan, error) {
	plan, err := g.parseOutput(output, codebasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan output: %w", err)
	}

	if _, err := g.state.SavePlan(plan); err != nil {
		return nil, fmt.Errorf("failed to save plan: %w", err)
	}

	return plan, nil
}

// Generate creates a new plan for the given codebase path
func (g *Generator) Generate(codebasePath string) (*state.Plan, error) {
	output := agent.StreamWithPrefix(g.agent, prompts.Plan, "Planner", agent.ColorCyan)
	return g.ParseAndSave(output, codebasePath)
}

func (g *Generator) parseOutput(output string, codebasePath string) (*state.Plan, error) {
	plan := &state.Plan{
		ID:           state.GenerateID(),
		Created:      time.Now(),
		CodebaseRoot: codebasePath,
		Agent:        g.agent.Name(),
	}

	// Extract project name
	nameRe := regexp.MustCompile(`PROJECT_NAME:\s*(.+)`)
	if match := nameRe.FindStringSubmatch(output); len(match) > 1 {
		plan.Name = strings.TrimSpace(match[1])
	} else {
		plan.Name = "unnamed-project"
	}

	// Extract overview - everything between OVERVIEW: and SUBSYSTEMS:
	overviewStart := strings.Index(output, "OVERVIEW:")
	subsystemsStart := strings.Index(output, "SUBSYSTEMS:")
	if overviewStart != -1 && subsystemsStart != -1 && subsystemsStart > overviewStart {
		plan.Overview = strings.TrimSpace(output[overviewStart+9 : subsystemsStart])
	}

	// Extract subsystems by splitting on "SUBSYSTEM:"
	parts := strings.Split(output, "SUBSYSTEM:")
	for i, part := range parts {
		if i == 0 {
			continue // Skip content before first SUBSYSTEM:
		}

		subsystem := parseSubsystemBlock(part)
		if subsystem != nil {
			plan.Subsystems = append(plan.Subsystems, *subsystem)
		}
	}

	if len(plan.Subsystems) == 0 {
		return nil, fmt.Errorf("no subsystems found in output")
	}

	return plan, nil
}

func parseSubsystemBlock(block string) *state.Subsystem {
	lines := strings.Split(block, "\n")
	if len(lines) == 0 {
		return nil
	}

	sub := &state.Subsystem{
		Slug: strings.TrimSpace(lines[0]),
	}

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "NAME:") {
			sub.Name = strings.TrimSpace(strings.TrimPrefix(line, "NAME:"))
		} else if strings.HasPrefix(line, "PATHS:") {
			sub.Paths = strings.TrimSpace(strings.TrimPrefix(line, "PATHS:"))
		} else if strings.HasPrefix(line, "DESCRIPTION:") {
			sub.Description = strings.TrimSpace(strings.TrimPrefix(line, "DESCRIPTION:"))
		} else if strings.HasPrefix(line, "INTERACTIONS:") {
			sub.Interactions = strings.TrimSpace(strings.TrimPrefix(line, "INTERACTIONS:"))
		}
	}

	// Validate we got the required fields
	if sub.Slug == "" || sub.Name == "" {
		return nil
	}

	return sub
}
