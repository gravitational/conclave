package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

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
