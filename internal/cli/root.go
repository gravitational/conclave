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
	useMulti  bool
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
	rootCmd.PersistentFlags().BoolVar(&useClaude, "claude", false, "Use/include Claude CLI")
	rootCmd.PersistentFlags().BoolVar(&useGemini, "gemini", false, "Use/include Gemini CLI")
	rootCmd.PersistentFlags().BoolVar(&useCodex, "codex", false, "Include Codex CLI (use with --claude/--gemini for multi-provider)")
	rootCmd.PersistentFlags().BoolVar(&useMulti, "multi", false, "Use all three providers (Codex, Claude, Gemini)")
}

func Execute() error {
	return rootCmd.Execute()
}

// enabledProviders returns a list of all enabled provider names
func enabledProviders() []string {
	// --multi enables all three
	if useMulti {
		return []string{"codex", "claude", "gemini"}
	}

	var providers []string

	// If no flags set, default to codex only
	if !useClaude && !useGemini && !useCodex {
		return []string{"codex"}
	}

	// Add explicitly enabled providers
	if useCodex {
		providers = append(providers, "codex")
	}
	if useClaude {
		providers = append(providers, "claude")
	}
	if useGemini {
		providers = append(providers, "gemini")
	}

	// If only one of claude/gemini set without --codex, use just that one
	// (backwards compatible behavior)
	if len(providers) == 0 {
		return []string{"codex"}
	}

	return providers
}

// PrimaryBackend returns the name of the primary agent backend (for single-agent operations)
func PrimaryBackend() string {
	if useClaude {
		return "Claude"
	}
	if useGemini {
		return "Gemini"
	}
	return "Codex"
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

// DistributeAgents returns n agents distributed across enabled providers
// Each agent is randomly assigned to one of the enabled providers
func DistributeAgents(n int) []agent.Agent {
	providers := enabledProviders()
	rand.Seed(time.Now().UnixNano())

	agents := make([]agent.Agent, n)

	if len(providers) == 1 {
		// Single provider - all agents use it
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

	// Create agents
	for i := 0; i < n; i++ {
		agents[i] = createAgentByName(assigned[i])
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
