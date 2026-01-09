package cli

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/spf13/cobra"
)

var (
	useClaude bool
	useGemini bool
	useCodex  bool
)

var rootCmd = &cobra.Command{
	Use:   "conclave",
	Short: "CLI agent orchestration tool for systematic codebase auditing",
	Long: `Conclave orchestrates multiple LLM agents to systematically audit codebases
for security vulnerabilities, logic flaws, and other critical issues.

The workflow consists of:
  1. plan    - Analyze codebase and identify subsystems
  2. assess  - Spin up agents to review subsystems in parallel
  3. convene - Have agents debate and refine their findings
  4. complete - Synthesize final results`,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&useClaude, "claude", false, "Use Claude CLI")
	rootCmd.PersistentFlags().BoolVar(&useGemini, "gemini", false, "Use Gemini CLI")
	rootCmd.PersistentFlags().BoolVar(&useCodex, "codex", false, "Use Codex CLI (default if no flags)")
}

func Execute() error {
	return rootCmd.Execute()
}

// enabledProviders returns a list of all enabled provider names
// Multiple flags = multiple providers with automatic failover
func enabledProviders() []string {
	var providers []string

	if useCodex {
		providers = append(providers, "codex")
	}
	if useClaude {
		providers = append(providers, "claude")
	}
	if useGemini {
		providers = append(providers, "gemini")
	}

	// Default to codex if no flags specified
	if len(providers) == 0 {
		return []string{"codex"}
	}

	return providers
}

// PrimaryBackend returns the name of the primary agent backend (first enabled provider)
func PrimaryBackend() string {
	providers := enabledProviders()
	return strings.Title(providers[0])
}

// AgentBackend returns a description of enabled backends
func AgentBackend() string {
	providers := enabledProviders()
	if len(providers) == 1 {
		return strings.Title(providers[0])
	}
	// Capitalize each
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = strings.Title(p)
	}
	return strings.Join(names, ", ")
}

// CreateAgent returns a new agent based on the primary backend
func CreateAgent() agent.Agent {
	return createAgentByName(strings.ToLower(PrimaryBackend()))
}

// CreateResilientAgent returns an agent with automatic failover to other providers
func CreateResilientAgent() agent.Agent {
	providers := enabledProviders()
	if len(providers) == 1 {
		return createAgentByName(providers[0])
	}

	// Primary is the first specified provider
	primary := createAgentByName(strings.ToLower(PrimaryBackend()))

	// Build fallback list from other enabled providers
	var fallbacks []agent.Agent
	primaryName := strings.ToLower(PrimaryBackend())
	for _, p := range providers {
		if p != primaryName {
			fallbacks = append(fallbacks, createAgentByName(p))
		}
	}

	return agent.NewResilientAgent(primary, fallbacks)
}

// createAgentByName creates an agent for the given provider name
func createAgentByName(name string) agent.Agent {
	switch name {
	case "claude":
		return agent.NewClaudeAgent()
	case "gemini":
		return agent.NewGeminiAgent()
	default:
		return agent.NewCodexAgent()
	}
}

// DistributeAgents returns n resilient agents distributed across enabled providers
// Each agent is randomly assigned to one of the enabled providers, with failover to others
func DistributeAgents(n int) []agent.Agent {
	providers := enabledProviders()
	rand.Seed(time.Now().UnixNano())

	agents := make([]agent.Agent, n)

	if len(providers) == 1 {
		// Single provider - all agents use it (no failover possible)
		for i := 0; i < n; i++ {
			agents[i] = createAgentByName(providers[0])
		}
		return agents
	}

	// Multiple providers - distribute ensuring each is used at least once if possible
	// First, assign one agent to each provider (up to n)
	assigned := make([]string, n)
	for i := 0; i < n && i < len(providers); i++ {
		assigned[i] = providers[i]
	}

	// Shuffle the initial assignments
	rand.Shuffle(len(assigned), func(i, j int) {
		if assigned[i] != "" && assigned[j] != "" {
			assigned[i], assigned[j] = assigned[j], assigned[i]
		}
	})

	// Fill remaining slots randomly
	for i := 0; i < n; i++ {
		if assigned[i] == "" {
			assigned[i] = providers[rand.Intn(len(providers))]
		}
	}

	// Shuffle again for good measure
	rand.Shuffle(n, func(i, j int) {
		assigned[i], assigned[j] = assigned[j], assigned[i]
	})

	// Create resilient agents with failover capability
	for i := 0; i < n; i++ {
		primary := createAgentByName(assigned[i])

		// Build fallback list from other providers
		var fallbacks []agent.Agent
		for _, p := range providers {
			if p != assigned[i] {
				fallbacks = append(fallbacks, createAgentByName(p))
			}
		}

		// Shuffle fallbacks for variety
		rand.Shuffle(len(fallbacks), func(a, b int) {
			fallbacks[a], fallbacks[b] = fallbacks[b], fallbacks[a]
		})

		agents[i] = agent.NewResilientAgent(primary, fallbacks)
	}

	return agents
}

// DescribeDistribution returns a string describing which agents use which providers
func DescribeDistribution(agents []agent.Agent) string {
	counts := make(map[string]int)
	for _, ag := range agents {
		counts[ag.Name()]++
	}

	var parts []string
	for name, count := range counts {
		parts = append(parts, fmt.Sprintf("%d×%s", count, strings.Title(name)))
	}
	return strings.Join(parts, ", ")
}

func printError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}

func printStatus(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}
