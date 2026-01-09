package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	useClaude bool
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
}

func Execute() error {
	return rootCmd.Execute()
}

func UseClaude() bool {
	return useClaude
}

func printError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}

func printStatus(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}
