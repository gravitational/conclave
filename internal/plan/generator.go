package plan

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/state"
)

const planPrompt = `You are analyzing a codebase to create a security audit plan. Your task is to:

1. Understand the overall architecture and purpose of the codebase
2. Identify distinct subsystems that should be reviewed independently
3. Document how these subsystems interact with each other

You are currently in the directory of the codebase to analyze. Please explore it thoroughly.

Your output MUST follow this exact format:

PROJECT_NAME: <short descriptive name for the project>

OVERVIEW:
<A 2-4 paragraph description of what this codebase does, its main technologies, and security-relevant architecture>

SUBSYSTEMS:

SUBSYSTEM: <slug-name>
NAME: <Human Readable Name>
PATHS: <comma-separated list of relevant paths>
DESCRIPTION: <what this subsystem does>
INTERACTIONS: <what other subsystems this interacts with>

SUBSYSTEM: <slug-name>
NAME: <Human Readable Name>
PATHS: <comma-separated list of relevant paths>
DESCRIPTION: <what this subsystem does>
INTERACTIONS: <what other subsystems this interacts with>

(continue for all subsystems)

Guidelines:
- Identify 3-10 subsystems depending on codebase size
- Focus on security-relevant boundaries (auth, data handling, external APIs, etc.)
- Use lowercase-with-hyphens for slug names
- Be specific about file paths
- Consider: authentication, authorization, data persistence, external integrations, user input handling, cryptography, session management, etc.
`

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

// Generate creates a new plan for the given codebase path
func (g *Generator) Generate(codebasePath string) (*state.Plan, error) {
	// Run agent to analyze codebase
	output := agent.StreamWithPrefix(g.agent, planPrompt, "Planner", agent.ColorCyan)

	// Parse the output
	plan, err := g.parseOutput(output, codebasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan output: %w", err)
	}

	// Save the plan
	if _, err := g.state.SavePlan(plan); err != nil {
		return nil, fmt.Errorf("failed to save plan: %w", err)
	}

	return plan, nil
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
