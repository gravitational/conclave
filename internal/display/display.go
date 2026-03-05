package display

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/term"
)

// capitalize returns the string with the first letter uppercased
func capitalizeStr(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// ANSI codes
const (
	ClearLine    = "\033[2K"
	MoveUp       = "\033[%dA"
	MoveToStart  = "\r"
	HideCursor   = "\033[?25l"
	ShowCursor   = "\033[?25h"
	Bold         = "\033[1m"
	Dim          = "\033[2m"
	Reset        = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Usage holds token consumption metrics for display
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostUSD      float64
}

// AgentStatus represents the current state of an agent
type AgentStatus struct {
	Name      string
	Provider  string
	Model     string // Specific model being used (optional)
	State     string // "waiting", "running", "done", "error"
	Activity  string // Current activity description
	Lines     int    // Lines of output received
	StartTime time.Time
	EndTime   time.Time // Set when done/error
	Error     error
	Usage     *Usage // Token usage (optional, set when complete)
}

// StatusDisplay manages a multi-agent status display
type StatusDisplay struct {
	mu           sync.Mutex
	agents       map[int]*AgentStatus
	agentOrder   []int
	numAgents    int
	spinnerIdx   int
	lastRender   int // number of lines last rendered
	stopSpinner  chan struct{}
	verbose      bool
	termWidth    int
}

// NewStatusDisplay creates a new status display for n agents
func NewStatusDisplay(n int, verbose bool) *StatusDisplay {
	width := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w
	}

	sd := &StatusDisplay{
		agents:      make(map[int]*AgentStatus),
		agentOrder:  make([]int, n),
		numAgents:   n,
		stopSpinner: make(chan struct{}),
		verbose:     verbose,
		termWidth:   width,
	}

	for i := 0; i < n; i++ {
		sd.agentOrder[i] = i
		sd.agents[i] = &AgentStatus{
			Name:      fmt.Sprintf("Agent %d", i+1),
			State:     "waiting",
			StartTime: time.Now(),
		}
	}

	return sd
}

// SetAgent configures an agent's name, provider, and model
func (sd *StatusDisplay) SetAgent(idx int, name, provider, model string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if status, ok := sd.agents[idx]; ok {
		status.Name = name
		status.Provider = provider
		status.Model = model
	}
}

// UpdateStatus updates an agent's status
func (sd *StatusDisplay) UpdateStatus(idx int, state, activity string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if status, ok := sd.agents[idx]; ok {
		status.State = state
		if activity != "" {
			status.Activity = activity
		}
	}
}

// AddLine records a new line of output for an agent
func (sd *StatusDisplay) AddLine(idx int, line string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if status, ok := sd.agents[idx]; ok {
		status.Lines++
		// Extract meaningful activity from the line
		activity := extractActivity(line)
		if activity != "" {
			status.Activity = activity
		}
		if status.State == "waiting" {
			status.State = "running"
		}
	}
}

// SetError marks an agent as errored
func (sd *StatusDisplay) SetError(idx int, err error) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if status, ok := sd.agents[idx]; ok {
		status.State = "error"
		status.Error = err
		status.EndTime = time.Now()
	}
}

// SetDone marks an agent as completed
func (sd *StatusDisplay) SetDone(idx int) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if status, ok := sd.agents[idx]; ok {
		status.State = "done"
		status.EndTime = time.Now()
	}
}

// SetUsage sets the token usage for an agent
func (sd *StatusDisplay) SetUsage(idx int, inputTokens, outputTokens, totalTokens int, costUSD float64) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if status, ok := sd.agents[idx]; ok {
		status.Usage = &Usage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  totalTokens,
			CostUSD:      costUSD,
		}
	}
}

// Start begins the status display with spinner animation
func (sd *StatusDisplay) Start() {
	fmt.Print(HideCursor)
	sd.render()

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-sd.stopSpinner:
				return
			case <-ticker.C:
				sd.mu.Lock()
				sd.spinnerIdx = (sd.spinnerIdx + 1) % len(spinnerFrames)
				sd.mu.Unlock()
				sd.render()
			}
		}
	}()
}

// Stop ends the status display
func (sd *StatusDisplay) Stop() {
	close(sd.stopSpinner)
	sd.render() // Final render
	fmt.Print(ShowCursor)
	fmt.Println() // Move past the display
}

// render draws the current status
func (sd *StatusDisplay) render() {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	// Move cursor up to overwrite previous render
	if sd.lastRender > 0 {
		fmt.Printf(MoveUp, sd.lastRender)
	}

	var lines []string
	spinner := spinnerFrames[sd.spinnerIdx]

	for _, idx := range sd.agentOrder {
		status := sd.agents[idx]
		line := sd.formatStatus(status, spinner)
		lines = append(lines, line)
	}

	for _, line := range lines {
		fmt.Print(ClearLine + line + "\n")
	}

	sd.lastRender = len(lines)
}

func (sd *StatusDisplay) formatStatus(status *AgentStatus, spinner string) string {
	// Color based on provider
	providerColor := ColorCyan
	switch strings.ToLower(status.Provider) {
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
	switch status.State {
	case "waiting":
		stateIndicator = spinner
		stateColor = Dim
	case "running":
		stateIndicator = spinner
		stateColor = ColorGreen
	case "done":
		stateIndicator = "✓"
		stateColor = ColorGreen
	case "error":
		stateIndicator = "✗"
		stateColor = ColorRed
	}

	// Build the status line - show provider with model if available
	displayName := status.Provider
	if displayName == "" {
		displayName = "..."
	} else if status.Model != "" {
		displayName = fmt.Sprintf("%s (%s)", capitalizeStr(displayName), status.Model)
	} else {
		displayName = capitalizeStr(displayName)
	}

	// Activity or state message
	activity := status.Activity
	if activity == "" {
		switch status.State {
		case "waiting":
			activity = "Starting..."
		case "done":
			activity = "Complete"
		case "error":
			if status.Error != nil {
				activity = status.Error.Error()
			} else {
				activity = "Failed"
			}
		}
	}

	// Duration - use EndTime if done/error, otherwise keep counting
	var elapsed time.Duration
	if !status.EndTime.IsZero() {
		elapsed = status.EndTime.Sub(status.StartTime)
	} else {
		elapsed = time.Since(status.StartTime)
	}
	duration := formatDuration(elapsed)

	// Format: ⠋ Agent 1 [Claude]  Analyzing auth... (42 lines, 1m23s)
	nameAndProvider := fmt.Sprintf("%s%-8s%s [%s%s%s]",
		Bold, status.Name, Reset,
		providerColor, displayName, Reset)

	// Truncate activity to fit
	maxActivity := sd.termWidth - 45
	if maxActivity < 20 {
		maxActivity = 20
	}
	if len(activity) > maxActivity {
		activity = activity[:maxActivity-3] + "..."
	}

	// Build stats string: lines, tokens, cost, duration
	var stats []string
	if status.Lines > 0 {
		stats = append(stats, fmt.Sprintf("%d lines", status.Lines))
	}
	if status.Usage != nil && status.Usage.TotalTokens > 0 {
		stats = append(stats, formatTokens(status.Usage.TotalTokens))
		if status.Usage.CostUSD > 0 {
			stats = append(stats, fmt.Sprintf("$%.2f", status.Usage.CostUSD))
		}
	}
	stats = append(stats, duration)

	return fmt.Sprintf(" %s%s%s %s  %s (%s)",
		stateColor, stateIndicator, Reset,
		nameAndProvider,
		activity,
		strings.Join(stats, ", "))
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// formatTokens formats a token count with K/M suffixes
func formatTokens(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM tokens", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fK tokens", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d tokens", tokens)
}

// extractActivity tries to extract a meaningful status from output
func extractActivity(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	// Skip common noise
	lower := strings.ToLower(line)
	noisePatterns := []string{
		"```", "---", "===", "***",
		"thinking", "let me", "i'll", "i will",
		"here's", "here is", "the following",
	}
	for _, p := range noisePatterns {
		if strings.HasPrefix(lower, p) {
			return ""
		}
	}

	// Look for file paths
	if strings.Contains(line, "/") || strings.Contains(line, ".go") ||
		strings.Contains(line, ".js") || strings.Contains(line, ".py") {
		// Might be a file reference
		if len(line) < 80 {
			return line
		}
	}

	// Look for action words
	actionPrefixes := []string{
		"Reading", "Analyzing", "Checking", "Reviewing",
		"Found", "Scanning", "Looking", "Examining",
		"Processing", "Evaluating", "Testing",
	}
	for _, prefix := range actionPrefixes {
		if strings.HasPrefix(line, prefix) {
			if len(line) > 60 {
				return line[:57] + "..."
			}
			return line
		}
	}

	// For shorter lines that might be section headers
	if len(line) < 50 && !strings.Contains(line, " ") {
		return line
	}

	return ""
}

// PrintHeader prints a section header
func PrintHeader(title string) {
	width := 60
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = min(w, 80)
	}

	fmt.Println()
	fmt.Printf("%s%s%s\n", Bold, strings.Repeat("─", width), Reset)
	fmt.Printf("%s%s%s\n", Bold, title, Reset)
	fmt.Printf("%s%s%s\n", Dim, strings.Repeat("─", width), Reset)
}

// PrintStatus prints a simple status message
func PrintStatus(format string, args ...interface{}) {
	fmt.Printf("%s%s%s\n", Dim, fmt.Sprintf(format, args...), Reset)
}

// PrintSuccess prints a success message
func PrintSuccess(format string, args ...interface{}) {
	fmt.Printf("%s✓%s %s\n", ColorGreen, Reset, fmt.Sprintf(format, args...))
}

// PrintError prints an error message
func PrintError(format string, args ...interface{}) {
	fmt.Printf("%s✗%s %s\n", ColorRed, Reset, fmt.Sprintf(format, args...))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// PrintPrompt prints a prompt with a header, truncated if needed
func PrintPrompt(name string, prompt string, maxLines int) {
	fmt.Printf("\n%s%s%s\n", ColorCyan, name, Reset)
	fmt.Printf("%s%s%s\n", Dim, strings.Repeat("─", 40), Reset)

	lines := strings.Split(prompt, "\n")
	shown := 0
	for _, line := range lines {
		if shown >= maxLines {
			fmt.Printf("%s  ... (%d more lines)%s\n", Dim, len(lines)-shown, Reset)
			break
		}
		// Truncate long lines
		if len(line) > 100 {
			line = line[:97] + "..."
		}
		fmt.Printf("%s%s%s\n", Dim, line, Reset)
		shown++
	}
}
