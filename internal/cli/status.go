package cli

import (
	"fmt"

	"github.com/anthropics/conclave/internal/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current state of analysis",
	Long:  `Display the current state of the conclave analysis including plans, assessments, and results.`,
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	st, err := state.New(".")
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	plans, err := st.ListPlans()
	if err != nil {
		return fmt.Errorf("failed to list plans: %w", err)
	}

	if len(plans) == 0 {
		printStatus("No plans found. Run 'conclave plan' to get started.")
		return nil
	}

	printStatus("Plans:")
	for _, p := range plans {
		printStatus("  - %s (%s)", p.Name, p.ID)
		printStatus("    Created: %s", p.Created.Format("2006-01-02 15:04:05"))
		printStatus("    Subsystems: %d", len(p.Subsystems))

		// Check assessment status for each subsystem
		for _, sub := range p.Subsystems {
			perspectives, _ := st.LoadPerspectives(p.ID, sub.Slug)
			debates, _ := st.LoadDebates(p.ID, sub.Slug)
			result, _ := st.LoadResult(p.ID, sub.Slug)

			status := "not started"
			if result != "" {
				status = "complete"
			} else if len(debates) > 0 {
				status = "debated"
			} else if len(perspectives) > 0 {
				status = "assessed"
			}

			printStatus("      - %s: %s", sub.Name, status)
		}
		printStatus("")
	}

	return nil
}
