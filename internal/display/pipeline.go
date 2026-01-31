package display

import (
	"fmt"
	"os"
	"sync"
	"time"
	"unicode"

	"golang.org/x/term"
)

// capitalize returns the string with the first letter uppercased
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// FindingStatus represents the current state of a finding in the pipeline
type FindingStatus struct {
	Name      string
	Provider  string
	Model     string // Specific model being used (optional)
	Phase     string // "steelman", "critique", "judge", "done"
	Activity  string
	Verdict   string // "RAISE" or "DISMISS" when done
	StartTime time.Time
	EndTime   time.Time
	Error     error
}

// PipelineDisplay manages a terminal display for pipelined findings
type PipelineDisplay struct {
	mu           sync.Mutex
	findings     map[int]*FindingStatus
	findingOrder []int
	numFindings  int
	spinnerIdx   int
	lastRender   int
	stopSpinner  chan struct{}
	termWidth    int
}

// NewPipelineDisplay creates a new pipeline display for n findings
func NewPipelineDisplay(n int, findingLabels []string) *PipelineDisplay {
	width := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w
	}

	pd := &PipelineDisplay{
		findings:     make(map[int]*FindingStatus),
		findingOrder: make([]int, n),
		numFindings:  n,
		stopSpinner:  make(chan struct{}),
		termWidth:    width,
	}

	for i := 0; i < n; i++ {
		pd.findingOrder[i] = i
		label := fmt.Sprintf("Finding %d", i+1)
		if i < len(findingLabels) {
			label = findingLabels[i]
		}
		pd.findings[i] = &FindingStatus{
			Name:      label,
			Phase:     "pending",
			StartTime: time.Now(),
		}
	}

	return pd
}

// SetPhase updates a finding's current phase, provider, and model
func (pd *PipelineDisplay) SetPhase(idx int, phase, provider, model string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if status, ok := pd.findings[idx]; ok {
		status.Phase = phase
		status.Provider = provider
		status.Model = model
		status.Activity = "Starting..."
	}
}

// SetActivity updates a finding's current activity
func (pd *PipelineDisplay) SetActivity(idx int, activity string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if status, ok := pd.findings[idx]; ok {
		status.Activity = activity
	}
}

// SetDone marks a finding as complete with its verdict
func (pd *PipelineDisplay) SetDone(idx int, verdict string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if status, ok := pd.findings[idx]; ok {
		status.Phase = "done"
		status.Verdict = verdict
		status.EndTime = time.Now()
	}
}

// SetError marks a finding as errored
func (pd *PipelineDisplay) SetError(idx int, err error) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if status, ok := pd.findings[idx]; ok {
		status.Error = err
		status.EndTime = time.Now()
	}
}

// Start begins the display with spinner animation
func (pd *PipelineDisplay) Start() {
	fmt.Print(HideCursor)
	pd.render()

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-pd.stopSpinner:
				return
			case <-ticker.C:
				pd.mu.Lock()
				pd.spinnerIdx = (pd.spinnerIdx + 1) % len(spinnerFrames)
				pd.mu.Unlock()
				pd.render()
			}
		}
	}()
}

// Stop ends the display
func (pd *PipelineDisplay) Stop() {
	close(pd.stopSpinner)
	pd.render() // Final render
	fmt.Print(ShowCursor)
	fmt.Println() // Move past the display
}

// render draws the current status
func (pd *PipelineDisplay) render() {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	// Move cursor up to overwrite previous render
	if pd.lastRender > 0 {
		fmt.Printf(MoveUp, pd.lastRender)
	}

	var lines []string
	spinner := spinnerFrames[pd.spinnerIdx]

	for _, idx := range pd.findingOrder {
		status := pd.findings[idx]
		line := pd.formatStatus(status, spinner)
		lines = append(lines, line)
	}

	for _, line := range lines {
		fmt.Print(ClearLine + line + "\n")
	}

	pd.lastRender = len(lines)
}

func (pd *PipelineDisplay) formatStatus(status *FindingStatus, spinner string) string {
	// Color based on provider
	providerColor := ColorCyan
	switch status.Provider {
	case "claude":
		providerColor = ColorMagenta
	case "gemini":
		providerColor = ColorBlue
	case "codex":
		providerColor = ColorGreen
	}

	// State indicator
	var stateIndicator string
	var stateColor string

	if status.Error != nil {
		stateIndicator = "✗"
		stateColor = ColorRed
	} else if status.Phase == "done" {
		stateIndicator = "✓"
		stateColor = ColorGreen
	} else {
		stateIndicator = spinner
		stateColor = ColorGreen
	}

	// Phase display
	phaseDisplay := status.Phase
	switch status.Phase {
	case "steelman":
		phaseDisplay = "Steel Man"
	case "critique":
		phaseDisplay = "Critique"
	case "judge":
		phaseDisplay = "Judge"
	case "done":
		phaseDisplay = "Done"
	case "pending":
		phaseDisplay = "Waiting"
		stateColor = Dim
	}

	// Provider display - show provider with model if available
	providerDisplay := status.Provider
	if providerDisplay == "" {
		providerDisplay = "..."
	} else if status.Model != "" {
		providerDisplay = fmt.Sprintf("%s (%s)", capitalize(providerDisplay), status.Model)
	} else {
		providerDisplay = capitalize(providerDisplay)
	}

	// Activity or verdict
	activity := status.Activity
	if status.Phase == "done" && status.Verdict != "" {
		if status.Verdict == "RAISE" {
			activity = fmt.Sprintf("%sRAISE%s", ColorGreen, Reset)
		} else {
			activity = fmt.Sprintf("%sDISMISS%s", Dim, Reset)
		}
	} else if status.Error != nil {
		activity = status.Error.Error()
	} else if activity == "" {
		activity = "Starting..."
	}

	// Duration
	var elapsed time.Duration
	if !status.EndTime.IsZero() {
		elapsed = status.EndTime.Sub(status.StartTime)
	} else {
		elapsed = time.Since(status.StartTime)
	}
	duration := formatDuration(elapsed)

	// Truncate activity to fit
	maxActivity := pd.termWidth - 50
	if maxActivity < 20 {
		maxActivity = 20
	}
	if len(activity) > maxActivity {
		activity = activity[:maxActivity-3] + "..."
	}

	// Format: ⠋ Finding 1 [Claude (opus)] Critique - Analyzing...  (1m23s)
	return fmt.Sprintf(" %s%s%s %s%-12s%s [%s%s%s] %s%-10s%s - %s (%s)",
		stateColor, stateIndicator, Reset,
		Bold, truncate(status.Name, 12), Reset,
		providerColor, providerDisplay, Reset,
		Dim, phaseDisplay, Reset,
		activity,
		duration)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
