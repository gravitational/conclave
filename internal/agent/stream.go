package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/web"
)

// GlobalRegistry is the shared agent registry for control purposes
var GlobalRegistry = NewRegistry()

// ANSI color codes
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"
)

var (
	// outputMutex ensures clean output when multiple agents stream simultaneously
	outputMutex sync.Mutex
)

// StreamWithPrefix runs an agent and streams output with a colored prefix
// Returns the collected output as a string
func StreamWithPrefix(ag Agent, prompt string, prefix string, color string) string {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")

		outputMutex.Lock()
		fmt.Printf("%s[%s]%s %s\n", color, prefix, ColorReset, line)
		outputMutex.Unlock()
	}

	if err := <-errCh; err != nil {
		outputMutex.Lock()
		fmt.Printf("%s[%s]%s Error: %v\n", color, prefix, ColorReset, err)
		outputMutex.Unlock()
	}

	return result.String()
}

// StreamWithStatus runs an agent and updates a status display
// Returns the collected output as a string
func StreamWithStatus(ag Agent, prompt string, idx int, sd *display.StatusDisplay) string {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		sd.AddLine(idx, line)
	}

	if err := <-errCh; err != nil {
		sd.SetError(idx, err)
		return result.String()
	}

	sd.SetDone(idx)
	return result.String()
}

// StreamWithWeb runs an agent and sends updates to web hub
func StreamWithWeb(ag Agent, prompt string, idx int, name string, hub *web.Hub) string {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder
	lineCount := 0
	startTime := time.Now()

	// Get model if available
	model := ""
	if m, ok := ag.(interface{ Model() string }); ok {
		model = m.Model()
	}

	// Initial status
	hub.UpdateAgent(&web.AgentStatusData{
		ID:        idx,
		Name:      name,
		Provider:  strings.Title(ag.Name()),
		Model:     model,
		State:     "running",
		Activity:  "Starting...",
		Lines:     0,
		StartTime: startTime,
	})

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		lineCount++

		// Send log
		hub.AddLog(idx, line)

		// Update status periodically
		activity := extractActivity(line)
		if activity == "" {
			activity = fmt.Sprintf("Processing... (%d lines)", lineCount)
		}

		hub.UpdateAgent(&web.AgentStatusData{
			ID:        idx,
			Name:      name,
			Provider:  strings.Title(ag.Name()),
			Model:     model,
			State:     "running",
			Activity:  activity,
			Lines:     lineCount,
			StartTime: startTime,
		})
	}

	endTime := time.Now()
	if err := <-errCh; err != nil {
		errStr := err.Error()
		hub.UpdateAgent(&web.AgentStatusData{
			ID:        idx,
			Name:      name,
			Provider:  strings.Title(ag.Name()),
			Model:     model,
			State:     "error",
			Activity:  errStr,
			Lines:     lineCount,
			StartTime: startTime,
			EndTime:   &endTime,
			Error:     errStr,
		})
		return result.String()
	}

	hub.UpdateAgent(&web.AgentStatusData{
		ID:        idx,
		Name:      name,
		Provider:  strings.Title(ag.Name()),
		Model:     model,
		State:     "done",
		Activity:  "Complete",
		Lines:     lineCount,
		StartTime: startTime,
		EndTime:   &endTime,
	})

	return result.String()
}

// StreamSilentResult holds output and any error from a silent stream
type StreamSilentResult struct {
	Content string
	Error   error
}

// StreamSilent runs an agent and collects output without displaying
// Shows a simple spinner instead
func StreamSilent(ag Agent, prompt string, description string) string {
	result := StreamSilentWithError(ag, prompt, description)
	return result.Content
}

// StreamSilentWithError runs an agent and collects output, returning both content and error
func StreamSilentWithError(ag Agent, prompt string, description string) StreamSilentResult {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder
	lineCount := 0

	// Simple inline status
	fmt.Printf("  %s...", description)

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		lineCount++

		// Update inline count occasionally
		if lineCount%10 == 0 {
			fmt.Printf("\r  %s... (%d lines)", description, lineCount)
		}
	}

	if err := <-errCh; err != nil {
		fmt.Printf("\r  %s... %s✗%s\n", description, ColorRed, ColorReset)
		return StreamSilentResult{Content: result.String(), Error: err}
	}

	fmt.Printf("\r  %s... %s✓%s (%d lines)\n", description, ColorGreen, ColorReset, lineCount)
	return StreamSilentResult{Content: result.String(), Error: nil}
}

// StreamSilentWithWeb runs an agent silently but sends updates to web hub
func StreamSilentWithWeb(ag Agent, prompt string, description string, hub *web.Hub) string {
	ctx, cancel := context.WithCancel(context.Background())

	model := ""
	if m, ok := ag.(interface{ Model() string }); ok {
		model = m.Model()
	}

	// Register single agent with ID 0
	GlobalRegistry.Clear()
	GlobalRegistry.Register(0, description, ag.Name(), model, cancel)
	defer GlobalRegistry.Unregister(0)

	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder
	lineCount := 0
	startTime := time.Now()

	// Use agent ID 0 for single-agent operations
	hub.UpdateAgent(&web.AgentStatusData{
		ID:        0,
		Name:      description,
		Provider:  strings.Title(ag.Name()),
		Model:     model,
		State:     "running",
		Activity:  "Starting...",
		Lines:     0,
		StartTime: startTime,
	})

	fmt.Printf("  %s...", description)

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		lineCount++

		hub.AddLog(0, line)

		if lineCount%10 == 0 {
			fmt.Printf("\r  %s... (%d lines)", description, lineCount)
			hub.UpdateAgent(&web.AgentStatusData{
				ID:        0,
				Name:      description,
				Provider:  strings.Title(ag.Name()),
				Model:     model,
				State:     "running",
				Activity:  fmt.Sprintf("Processing... (%d lines)", lineCount),
				Lines:     lineCount,
				StartTime: startTime,
			})
		}
	}

	endTime := time.Now()
	if err := <-errCh; err != nil {
		// Check if this was a cancellation (kill)
		state := "error"
		errStr := err.Error()
		if ctx.Err() == context.Canceled {
			state = "killed"
			errStr = "Killed by user"
		}
		fmt.Printf("\r  %s... %s✗%s\n", description, ColorRed, ColorReset)
		hub.UpdateAgent(&web.AgentStatusData{
			ID:        0,
			Name:      description,
			Provider:  strings.Title(ag.Name()),
			Model:     model,
			State:     state,
			Activity:  errStr,
			Lines:     lineCount,
			StartTime: startTime,
			EndTime:   &endTime,
			Error:     errStr,
		})
		return result.String()
	}

	fmt.Printf("\r  %s... %s✓%s (%d lines)\n", description, ColorGreen, ColorReset, lineCount)
	hub.UpdateAgent(&web.AgentStatusData{
		ID:        0,
		Name:      description,
		Provider:  strings.Title(ag.Name()),
		Model:     model,
		State:     "done",
		Activity:  "Complete",
		Lines:     lineCount,
		StartTime: startTime,
		EndTime:   &endTime,
	})

	return result.String()
}

// StreamMultiple runs multiple agents in parallel with different prefixes
func StreamMultiple(agents []Agent, prompts []string, prefixes []string, colors []string) []string {
	if len(agents) != len(prompts) || len(agents) != len(prefixes) || len(agents) != len(colors) {
		panic("mismatched slice lengths")
	}

	results := make([]string, len(agents))
	var wg sync.WaitGroup

	for i := range agents {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = StreamWithPrefix(agents[idx], prompts[idx], prefixes[idx], colors[idx])
		}(i)
	}

	wg.Wait()
	return results
}

// StreamMultipleWithStatus runs multiple agents with a unified status display
func StreamMultipleWithStatus(agents []Agent, prompts []string, names []string) []AgentResult {
	n := len(agents)
	if len(prompts) != n || len(names) != n {
		panic("mismatched slice lengths")
	}

	results := make([]AgentResult, n)
	sd := display.NewStatusDisplay(n, false)

	// Configure agents
	for i, ag := range agents {
		sd.SetAgent(i, names[i], ag.Name())
	}

	sd.Start()

	var wg sync.WaitGroup
	for i := range agents {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			content := StreamWithStatus(agents[idx], prompts[idx], idx, sd)
			results[idx] = AgentResult{
				Content: content,
				Agent:   GetMeta(agents[idx]),
			}
		}(i)
	}

	wg.Wait()
	sd.Stop()

	return results
}

// StreamMultipleWithWeb runs multiple agents with web dashboard updates
func StreamMultipleWithWeb(agents []Agent, prompts []string, names []string, hub *web.Hub) []AgentResult {
	n := len(agents)
	if len(prompts) != n || len(names) != n {
		panic("mismatched slice lengths")
	}

	results := make([]AgentResult, n)
	contexts := make([]context.Context, n)
	cancels := make([]context.CancelFunc, n)

	// Clear registry for fresh run
	GlobalRegistry.Clear()

	// Initialize all agents as waiting and create cancellable contexts
	for i, ag := range agents {
		ctx, cancel := context.WithCancel(context.Background())
		contexts[i] = ctx
		cancels[i] = cancel

		model := ""
		if m, ok := ag.(interface{ Model() string }); ok {
			model = m.Model()
		}

		// Register agent with the registry
		GlobalRegistry.Register(i, names[i], ag.Name(), model, cancel)

		hub.UpdateAgent(&web.AgentStatusData{
			ID:        i,
			Name:      names[i],
			Provider:  strings.Title(ag.Name()),
			Model:     model,
			State:     "waiting",
			Activity:  "Queued",
			Lines:     0,
			StartTime: time.Now(),
		})
	}

	var wg sync.WaitGroup
	for i := range agents {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer GlobalRegistry.Unregister(idx)
			content := StreamWithWebCtx(contexts[idx], agents[idx], prompts[idx], idx, names[idx], hub)
			results[idx] = AgentResult{
				Content: content,
				Agent:   GetMeta(agents[idx]),
			}
		}(i)
	}

	wg.Wait()
	return results
}

// StreamWithWebCtx runs an agent with context and sends updates to web hub
func StreamWithWebCtx(ctx context.Context, ag Agent, prompt string, idx int, name string, hub *web.Hub) string {
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder
	lineCount := 0
	startTime := time.Now()

	// Get model if available
	model := ""
	if m, ok := ag.(interface{ Model() string }); ok {
		model = m.Model()
	}

	// Initial status
	hub.UpdateAgent(&web.AgentStatusData{
		ID:        idx,
		Name:      name,
		Provider:  strings.Title(ag.Name()),
		Model:     model,
		State:     "running",
		Activity:  "Starting...",
		Lines:     0,
		StartTime: startTime,
	})

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		lineCount++

		// Send log
		hub.AddLog(idx, line)

		// Update status periodically
		activity := extractActivity(line)
		if activity == "" {
			activity = fmt.Sprintf("Processing... (%d lines)", lineCount)
		}

		hub.UpdateAgent(&web.AgentStatusData{
			ID:        idx,
			Name:      name,
			Provider:  strings.Title(ag.Name()),
			Model:     model,
			State:     "running",
			Activity:  activity,
			Lines:     lineCount,
			StartTime: startTime,
		})
	}

	endTime := time.Now()
	if err := <-errCh; err != nil {
		// Check if this was a cancellation (kill)
		state := "error"
		errStr := err.Error()
		if ctx.Err() == context.Canceled {
			state = "killed"
			errStr = "Killed by user"
		}
		hub.UpdateAgent(&web.AgentStatusData{
			ID:        idx,
			Name:      name,
			Provider:  strings.Title(ag.Name()),
			Model:     model,
			State:     state,
			Activity:  errStr,
			Lines:     lineCount,
			StartTime: startTime,
			EndTime:   &endTime,
			Error:     errStr,
		})
		return result.String()
	}

	hub.UpdateAgent(&web.AgentStatusData{
		ID:        idx,
		Name:      name,
		Provider:  strings.Title(ag.Name()),
		Model:     model,
		State:     "done",
		Activity:  "Complete",
		Lines:     lineCount,
		StartTime: startTime,
		EndTime:   &endTime,
	})

	return result.String()
}

// extractActivity extracts meaningful activity from a log line
func extractActivity(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	// Skip noise
	lower := strings.ToLower(line)
	noisePatterns := []string{"```", "---", "===", "***", "thinking", "let me", "i'll", "i will"}
	for _, p := range noisePatterns {
		if strings.HasPrefix(lower, p) {
			return ""
		}
	}

	// Look for file paths or action words
	if strings.Contains(line, "/") || strings.Contains(line, ".go") || strings.Contains(line, ".js") {
		if len(line) < 80 {
			return line
		}
	}

	actionPrefixes := []string{"Reading", "Analyzing", "Checking", "Reviewing", "Found", "Scanning", "Looking", "Examining", "Processing"}
	for _, prefix := range actionPrefixes {
		if strings.HasPrefix(line, prefix) {
			if len(line) > 60 {
				return line[:57] + "..."
			}
			return line
		}
	}

	return ""
}
