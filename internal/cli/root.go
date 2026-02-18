package cli

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/config"
	"github.com/spf13/cobra"
)

var (
	claudeModel string
	geminiModel string
	codexModel  string
	verbose     bool
	profileName string

	// runtimeConfig holds the resolved configuration for this run
	runtimeConfig *config.RuntimeConfig

	// globalConfig holds the loaded config file
	globalConfig *config.Config
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
  4. complete - Synthesize final results

You must specify at least one provider: --claude, --codex, or --gemini`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	// String flags with optional values: --claude or --claude=opus
	rootCmd.PersistentFlags().StringVar(&claudeModel, "claude", "", "Use Claude CLI (optionally specify model: --claude=opus)")
	rootCmd.PersistentFlags().StringVar(&geminiModel, "gemini", "", "Use Gemini CLI (optionally specify model: --gemini=gemini-2.5-pro)")
	rootCmd.PersistentFlags().StringVar(&codexModel, "codex", "", "Use Codex CLI (optionally specify model:effort, e.g., --codex=o3:high)")

	// Allow flags without values (--claude means use default model)
	rootCmd.PersistentFlags().Lookup("claude").NoOptDefVal = "default"
	rootCmd.PersistentFlags().Lookup("gemini").NoOptDefVal = "default"
	rootCmd.PersistentFlags().Lookup("codex").NoOptDefVal = "default"

	// Profile flag for config-based model selection
	rootCmd.PersistentFlags().StringVar(&profileName, "profile", "", "Use named profile from ~/.conclave/config.yaml (default profile used if config exists)")

	// Verbose flag for showing all stderr output
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show all stderr output from agents (default: only errors)")

	// Initialize config on any command that needs providers
	rootCmd.PersistentPreRunE = initConfig
}

// initConfig loads the config file and initializes runtimeConfig
func initConfig(cmd *cobra.Command, args []string) error {
	// Load config file (nil if doesn't exist)
	var err error
	globalConfig, err = config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// If CLI flags specified, they take precedence (skip profile)
	if len(enabledProviders()) > 0 {
		runtimeConfig = buildRuntimeConfigFromCLI()
		return nil
	}

	// No CLI flags - try to use profile
	if globalConfig != nil {
		// Determine which profile to use
		targetProfile := profileName
		if targetProfile == "" {
			targetProfile = "default"
		}

		profile := globalConfig.GetProfile(targetProfile)
		if profile == nil {
			// Profile not found
			if profileName != "" {
				// User explicitly requested a profile that doesn't exist
				names := globalConfig.ProfileNames()
				sort.Strings(names)
				return fmt.Errorf("profile %q not found. Available profiles: %v", profileName, names)
			}
			// No explicit profile and no "default" - fall through to CLI validation
		} else {
			runtimeConfig = config.NewRuntimeConfig(profile, verbose)
			return nil
		}
	} else if profileName != "" {
		// User requested profile but no config file exists
		return fmt.Errorf("--profile specified but no config file found at %s", config.DefaultConfigPath())
	}

	// No config or profile - runtimeConfig stays nil, will fail provider validation
	return nil
}

// buildRuntimeConfigFromCLI creates a RuntimeConfig from CLI flags
func buildRuntimeConfigFromCLI() *config.RuntimeConfig {
	providers := enabledProviders()
	if len(providers) == 0 {
		return nil
	}

	// Use primary provider for all stages
	primary := providers[0]
	spec := config.ModelSpec{
		Provider: primary,
		Model:    getModel(primary),
	}
	if primary == "codex" {
		spec.Effort = getCodexReasoningEffort()
	}

	// Build assess specs from all providers
	var assessSpecs []config.ModelSpec
	for _, p := range providers {
		s := config.ModelSpec{
			Provider: p,
			Model:    getModel(p),
		}
		if p == "codex" {
			s.Effort = getCodexReasoningEffort()
		}
		assessSpecs = append(assessSpecs, s)
	}
	// Pad to 3 if needed
	for len(assessSpecs) < 3 {
		assessSpecs = append(assessSpecs, spec)
	}

	return &config.RuntimeConfig{
		Verbose: verbose,
		Stages: config.StageConfig{
			Plan:     spec,
			Assess:   assessSpecs,
			Complete: spec,
			Convene: config.ConveneConfig{
				SteelMan: spec,
				Critique: spec,
				Judge:    spec,
			},
		},
	}
}

func Execute() error {
	return rootCmd.Execute()
}

// enabledProviders returns a list of all enabled provider names
// Multiple flags = multiple providers with automatic failover
func enabledProviders() []string {
	var providers []string

	if codexModel != "" {
		providers = append(providers, "codex")
	}
	if claudeModel != "" {
		providers = append(providers, "claude")
	}
	if geminiModel != "" {
		providers = append(providers, "gemini")
	}

	return providers
}

// ValidateProviders checks that at least one provider flag is specified or profile is configured
func ValidateProviders() error {
	// If runtime config is configured via profile, we're good
	if runtimeConfig != nil && runtimeConfig.IsConfigured() {
		return nil
	}
	// Otherwise check CLI flags
	if len(enabledProviders()) == 0 {
		return fmt.Errorf("no provider specified. Use at least one of: --claude, --codex, --gemini, or --profile")
	}
	return nil
}

// validateProvidersPreRun is a cobra PreRunE compatible version of ValidateProviders
func validateProvidersPreRun(cmd *cobra.Command, args []string) error {
	return ValidateProviders()
}

// GetRuntimeConfig returns the resolved runtime configuration
func GetRuntimeConfig() *config.RuntimeConfig {
	return runtimeConfig
}

// getModel returns the model for a provider, or empty string for default
func getModel(provider string) string {
	var model string
	switch provider {
	case "codex":
		model = codexModel
	case "claude":
		model = claudeModel
	case "gemini":
		model = geminiModel
	}
	if model == "default" {
		return ""
	}
	// For codex, strip reasoning effort suffix if present (e.g., "o3:high" -> "o3")
	if provider == "codex" && strings.Contains(model, ":") {
		parts := strings.SplitN(model, ":", 2)
		return parts[0]
	}
	return model
}

// getCodexReasoningEffort extracts reasoning effort from codex model string
// e.g., "o3:high" -> "high", "o3" -> ""
func getCodexReasoningEffort() string {
	if codexModel == "" || codexModel == "default" {
		return ""
	}
	if strings.Contains(codexModel, ":") {
		parts := strings.SplitN(codexModel, ":", 2)
		return parts[1]
	}
	return ""
}

// PrimaryBackend returns the name of the primary agent backend (first enabled provider)
func PrimaryBackend() string {
	providers := enabledProviders()
	return strings.Title(providers[0])
}

// AgentBackend returns a description of enabled backends with models
func AgentBackend() string {
	providers := enabledProviders()
	names := make([]string, len(providers))
	for i, p := range providers {
		model := getModel(p)
		if model != "" {
			// For codex, also show reasoning effort if set
			if p == "codex" {
				effort := getCodexReasoningEffort()
				if effort != "" {
					names[i] = fmt.Sprintf("%s (%s, effort=%s)", strings.Title(p), model, effort)
				} else {
					names[i] = fmt.Sprintf("%s (%s)", strings.Title(p), model)
				}
			} else {
				names[i] = fmt.Sprintf("%s (%s)", strings.Title(p), model)
			}
		} else {
			names[i] = strings.Title(p)
		}
	}
	return strings.Join(names, ", ")
}

// CreateAgent returns a new agent based on the primary backend
func CreateAgent() agent.Agent {
	name := strings.ToLower(enabledProviders()[0])
	return createAgentByName(name, getModel(name))
}

// CreateResilientAgent returns an agent with automatic failover to other providers
func CreateResilientAgent() agent.Agent {
	providers := enabledProviders()
	if len(providers) == 1 {
		return createAgentByName(providers[0], getModel(providers[0]))
	}

	// Primary is the first specified provider
	primaryName := providers[0]
	primary := createAgentByName(primaryName, getModel(primaryName))

	// Build fallback list from other enabled providers
	var fallbacks []agent.Agent
	for _, p := range providers[1:] {
		fallbacks = append(fallbacks, createAgentByName(p, getModel(p)))
	}

	return agent.NewResilientAgent(primary, fallbacks)
}

// createAgentByName creates an agent for the given provider name and model
func createAgentByName(name, model string) agent.Agent {
	switch name {
	case "claude":
		return agent.NewClaudeAgent(model, verbose)
	case "gemini":
		return agent.NewGeminiAgent(model, verbose)
	default:
		return agent.NewCodexAgent(model, getCodexReasoningEffort(), verbose)
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
		p := providers[0]
		for i := 0; i < n; i++ {
			agents[i] = createAgentByName(p, getModel(p))
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
		primary := createAgentByName(assigned[i], getModel(assigned[i]))

		// Build fallback list from other providers
		var fallbacks []agent.Agent
		for _, p := range providers {
			if p != assigned[i] {
				fallbacks = append(fallbacks, createAgentByName(p, getModel(p)))
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
