package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/prompts"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review files one-by-one for logic bugs",
	Long: `Read file paths from stdin (one per line) and review each file
individually for business logic flaws. Reports at most one bug per file.

Example:
  find . -name '*.go' | conclave --claude review
  git diff --name-only main | conclave --claude review`,
	PreRunE: validateProvidersPreRun,
	RunE:    runReview,
}

func init() {
	rootCmd.AddCommand(reviewCmd)
}

func runReview(cmd *cobra.Command, args []string) error {
	// Read file paths from stdin
	var files []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			files = append(files, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no files provided on stdin")
	}

	// Reset session usage tracking
	agent.GlobalSession.Reset()

	cfg := GetRuntimeConfig()

	display.PrintHeader("FILE-BY-FILE LOGIC REVIEW")
	if cfg != nil && cfg.IsConfigured() {
		display.PrintStatus("Provider: %s", cfg.AgentBackend())
	} else {
		display.PrintStatus("Provider: %s", AgentBackend())
	}
	display.PrintStatus("Files: %d", len(files))
	fmt.Println()

	// Build agents and prompts for all files in parallel
	n := len(files)
	agents := make([]agent.Agent, n)
	agentPrompts := make([]string, n)
	names := make([]string, n)

	for i, filePath := range files {
		agentPrompts[i] = prompts.Render(prompts.Review, map[string]any{
			"FilePath": filePath,
		})
		names[i] = filepath.Base(filePath)
		if cfg != nil && cfg.IsConfigured() {
			agents[i] = cfg.PlanAgent()
		} else {
			agents[i] = CreateAgent()
		}
	}

	results := agent.StreamMultipleWithStatus(agents, agentPrompts, names)

	fmt.Println()

	// Report findings
	var findings int
	for i, result := range results {
		content := strings.TrimSpace(result.Content)
		if content == "" || strings.Contains(content, "NO_BUGS_FOUND") {
			continue
		}
		findings++
		display.PrintHeader(files[i])
		fmt.Println(content)
		fmt.Println()
	}

	display.PrintHeader("REVIEW COMPLETE")
	display.PrintStatus("Files reviewed: %d", len(files))
	display.PrintStatus("Files with findings: %d", findings)

	return nil
}
