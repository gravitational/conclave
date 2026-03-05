package config

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
)

// RuntimeConfig holds the resolved configuration for a run
type RuntimeConfig struct {
	Verbose      bool
	Instructions []string
	Stages       StageConfig
	ProfileName  string // Name of the profile being used, if any
}

// NewRuntimeConfig creates a new RuntimeConfig from a config and profile
func NewRuntimeConfig(cfg *Config, profile *Profile, verbose bool) *RuntimeConfig {
	rc := &RuntimeConfig{
		Verbose: verbose,
	}
	if cfg != nil {
		rc.Instructions = cfg.Instructions
	}
	if profile != nil {
		rc.Stages = profile.Stages
		rc.ProfileName = profile.Name
	}
	return rc
}

// IsConfigured returns true if the runtime config has valid stage configuration
func (r *RuntimeConfig) IsConfigured() bool {
	return r != nil && !r.Stages.Plan.IsEmpty()
}

// CreateAgentFromSpec creates an agent from a model specification
func (r *RuntimeConfig) CreateAgentFromSpec(spec ModelSpec) agent.Agent {
	switch spec.Provider {
	case "claude":
		return agent.NewClaudeAgent(spec.Model, r.Verbose)
	case "gemini":
		return agent.NewGeminiAgent(spec.Model, r.Verbose)
	case "codex":
		return agent.NewCodexAgent(spec.Model, spec.Effort, r.Verbose)
	default:
		// Fallback to claude
		return agent.NewClaudeAgent(spec.Model, r.Verbose)
	}
}

// PlanAgent returns an agent for the plan stage
func (r *RuntimeConfig) PlanAgent() agent.Agent {
	return r.CreateAgentFromSpec(r.Stages.Plan)
}

// CompleteAgent returns an agent for the complete stage
func (r *RuntimeConfig) CompleteAgent() agent.Agent {
	spec := r.Stages.Complete
	if spec.IsEmpty() {
		// Fallback to plan spec
		spec = r.Stages.Plan
	}
	return r.CreateAgentFromSpec(spec)
}

// AssessCount returns how many assessor agents to run
func (r *RuntimeConfig) AssessCount() int {
	if len(r.Stages.Assess) == 0 {
		return 3 // default
	}
	return len(r.Stages.Assess)
}

// AssessAgents returns agents for the assess stage
// The list length is determined by the config (defaults to 3)
func (r *RuntimeConfig) AssessAgents() []agent.Agent {
	specs := r.Stages.Assess
	if len(specs) == 0 {
		// Fallback: 3 agents using plan spec
		planSpec := r.Stages.Plan
		specs = []ModelSpec{planSpec, planSpec, planSpec}
	}

	agents := make([]agent.Agent, len(specs))
	for i, spec := range specs {
		agents[i] = r.createResilientAgent(spec, specs)
	}
	return agents
}

// createResilientAgent creates an agent with fallback to other specs in the list
func (r *RuntimeConfig) createResilientAgent(primary ModelSpec, allSpecs []ModelSpec) agent.Agent {
	// Build fallback list from other providers in specs
	var fallbacks []agent.Agent
	seen := make(map[string]bool)
	seen[primary.Provider] = true

	for _, spec := range allSpecs {
		if !seen[spec.Provider] {
			seen[spec.Provider] = true
			fallbacks = append(fallbacks, r.CreateAgentFromSpec(spec))
		}
	}

	// Shuffle fallbacks
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(fallbacks), func(i, j int) {
		fallbacks[i], fallbacks[j] = fallbacks[j], fallbacks[i]
	})

	primaryAgent := r.CreateAgentFromSpec(primary)
	if len(fallbacks) == 0 {
		return primaryAgent
	}
	return agent.NewResilientAgent(primaryAgent, fallbacks)
}

// SteelManAgent returns an agent for the steel man phase
func (r *RuntimeConfig) SteelManAgent() agent.Agent {
	spec := r.Stages.Convene.SteelMan
	if spec.IsEmpty() {
		spec = r.Stages.Plan
	}
	return r.CreateAgentFromSpec(spec)
}

// CritiqueAgent returns an agent for the critique phase
func (r *RuntimeConfig) CritiqueAgent() agent.Agent {
	spec := r.Stages.Convene.Critique
	if spec.IsEmpty() {
		spec = r.Stages.Plan
	}
	return r.CreateAgentFromSpec(spec)
}

// JudgeAgent returns an agent for the judge phase
func (r *RuntimeConfig) JudgeAgent() agent.Agent {
	spec := r.Stages.Convene.Judge
	if spec.IsEmpty() {
		spec = r.Stages.Plan
	}
	return r.CreateAgentFromSpec(spec)
}

// AgentBackend returns a description of the providers/models being used
func (r *RuntimeConfig) AgentBackend() string {
	if r.ProfileName != "" {
		return fmt.Sprintf("Profile: %s", r.ProfileName)
	}

	// Collect unique providers
	specs := []ModelSpec{r.Stages.Plan, r.Stages.Complete}
	specs = append(specs, r.Stages.Assess...)
	specs = append(specs, r.Stages.Convene.SteelMan, r.Stages.Convene.Critique, r.Stages.Convene.Judge)

	seen := make(map[string]bool)
	var names []string
	for _, spec := range specs {
		if spec.IsEmpty() {
			continue
		}
		key := spec.Provider
		if spec.Model != "" {
			key = fmt.Sprintf("%s (%s)", spec.Provider, spec.Model)
		}
		if !seen[key] {
			seen[key] = true
			names = append(names, strings.Title(key))
		}
	}

	if len(names) == 0 {
		return "No provider configured"
	}
	return strings.Join(names, ", ")
}

// PrimaryBackend returns the primary backend name (from plan stage)
func (r *RuntimeConfig) PrimaryBackend() string {
	if r.Stages.Plan.IsEmpty() {
		return "Unknown"
	}
	return strings.Title(r.Stages.Plan.Provider)
}
