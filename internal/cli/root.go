package cli

import (
	"fmt"
	"os"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/spf13/cobra"
)

var (
	useClaude bool
	useGemini bool
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
	rootCmd.PersistentFlags().BoolVar(&useClaude, "claude", false, "Use Claude CLI instead of Codex")
	rootCmd.PersistentFlags().BoolVar(&useGemini, "gemini", false, "Use Gemini CLI instead of Codex")
}

func Execute() error {
	return rootCmd.Execute()
}

func UseClaude() bool {
	return useClaude
}

func UseGemini() bool {
	return useGemini
}

// AgentBackend returns the name of the selected agent backend
func AgentBackend() string {
	if useClaude {
		return "Claude"
	}
	if useGemini {
		return "Gemini"
	}
	return "Codex"
}

// CreateAgent returns a new agent based on the selected backend
func CreateAgent() agent.Agent {
	if useClaude {
		return agent.NewClaudeAgent()
	}
	if useGemini {
		return agent.NewGeminiAgent()
	}
	return agent.NewCodexAgent()
}

func printError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}

func printStatus(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}
