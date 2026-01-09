package cli

import (
	"fmt"

	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/state"
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

	display.PrintHeader("STATUS")

	if len(plans) == 0 {
		display.PrintStatus("No plans found. Run 'conclave plan' to get started.")
		return nil
	}

	for _, p := range plans {
		fmt.Printf("\n%s%s%s (%s)\n", display.Bold, p.Name, display.Reset, p.ID[:8])
		display.PrintStatus("Created: %s", p.Created.Format("2006-01-02 15:04"))
		fmt.Println()

		for _, sub := range p.Subsystems {
			perspectives, _ := st.LoadPerspectives(p.ID, sub.Slug)
			debates, _ := st.LoadDebates(p.ID, sub.Slug)
			result, _ := st.LoadResult(p.ID, sub.Slug)

			var status, color string
			if result != "" {
				status = "✓ complete"
				color = display.ColorGreen
			} else if len(debates) > 0 {
				status = "◐ debated"
				color = display.ColorYellow
			} else if len(perspectives) > 0 {
				status = "◐ assessed"
				color = display.ColorYellow
			} else {
				status = "○ pending"
				color = display.Dim
			}

			fmt.Printf("  %s%-20s%s %s%s%s\n", display.Dim, sub.Slug, display.Reset, color, status, display.Reset)
		}
	}
	fmt.Println()

	return nil
}
