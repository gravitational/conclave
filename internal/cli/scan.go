package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rob-picard-teleport/conclave/internal/agent"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/scan"
	"github.com/rob-picard-teleport/conclave/internal/web"
	"github.com/spf13/cobra"
)

var (
	scanUseWeb     bool
	scanCreateGist bool
)

var scanCmd = &cobra.Command{
	Use:   "scan <github-url|file>",
	Short: "Scan for security issues in PRs or regressions of known vulnerabilities",
	Long: `Two modes of operation:

1. PR Diff Scan (when given a PR URL):
   Analyzes the specific code changes in a pull request for security vulnerabilities.

2. Vulnerability Regression Scan (when given an issue URL or local file):
   Analyzes a vulnerability description and scans the codebase for:
   - Regressions of the original fix
   - Similar vulnerable patterns elsewhere
   - Related security issues

Examples:
  # Scan a PR's changes for security issues
  conclave --claude scan https://github.com/org/repo/pull/456

  # Scan for regressions of a known vulnerability
  conclave --claude scan https://github.com/org/repo/issues/123
  conclave --claude scan vuln-description.md`,
	Args:    cobra.ExactArgs(1),
	PreRunE: validateProvidersPreRun,
	RunE:    runScan,
}

func init() {
	scanCmd.Flags().BoolVar(&scanUseWeb, "web", false, "Open web dashboard for monitoring")
	scanCmd.Flags().BoolVar(&scanCreateGist, "gist", false, "Create a secret gist of the final report")
	rootCmd.AddCommand(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	input := args[0]

	// Start web dashboard if requested
	var hub *web.Hub
	if scanUseWeb {
		hub = web.NewHub()
		hub.SetControllers(
			agent.GlobalRegistry.Kill,
			agent.GlobalRegistry.KillAll,
		)
		go hub.Run()

		server := web.NewServer(hub)
		url, err := server.Start()
		if err != nil {
			return fmt.Errorf("failed to start web server: %w", err)
		}

		fmt.Printf("\n  Dashboard: %s\n\n", url)
		openBrowser(url)
	}

	// Detect if this is a PR URL - if so, use PR diff scan mode
	if isPullRequestURL(input) {
		return runPRScan(input, hub)
	}

	// Otherwise, use vulnerability regression scan mode
	return runVulnScan(input, hub)
}

// isPullRequestURL checks if the input is a GitHub PR URL
func isPullRequestURL(input string) bool {
	return strings.HasPrefix(input, "https://github.com/") && strings.Contains(input, "/pull/")
}

// runPRScan scans the specific changes in a pull request
func runPRScan(prURL string, hub *web.Hub) error {
	display.PrintHeader("PR SECURITY SCAN")
	display.PrintStatus("Providers: %s", AgentBackend())
	display.PrintStatus("PR: %s", prURL)
	fmt.Println()

	// Step 1: Load PR info and diff
	if hub != nil {
		hub.SetPhase("load", "Loading PR information")
	}
	display.PrintStatus("Step 1: Loading PR information...")

	prInfo, err := loadPRInfo(prURL)
	if err != nil {
		return fmt.Errorf("failed to load PR: %w", err)
	}

	display.PrintSuccess("PR: %s", prInfo.Title)
	display.PrintStatus("  Author: %s", prInfo.Author)
	display.PrintStatus("  Base: %s", prInfo.BaseBranch)
	display.PrintStatus("  Files changed: %d", len(prInfo.Files))
	fmt.Println()

	// Step 2: Threat Model
	if hub != nil {
		hub.SetPhase("threat-model", "Threat modeling PR")
	}
	display.PrintHeader("STEP 2: THREAT MODEL")

	threatModelPrompt := scan.ThreatModelPrompt(prInfo)
	var tmOutput string
	if hub != nil {
		tmOutput = agent.StreamSilentWithWeb(CreateAgent(), threatModelPrompt, "Analyzing threats", hub)
	} else {
		tmOutput = agent.StreamSilent(CreateAgent(), threatModelPrompt, "Analyzing threats")
	}

	threatModel := scan.ParseThreatModel(tmOutput)
	display.PrintSuccess("Threat Model:")
	display.PrintStatus("  %s", threatModel.Summary)
	if len(threatModel.Threats) > 0 {
		display.PrintStatus("  Threats identified: %d", len(threatModel.Threats))
		for i, t := range threatModel.Threats {
			if i >= 3 {
				display.PrintStatus("    ... and %d more", len(threatModel.Threats)-3)
				break
			}
			display.PrintStatus("    %d. %s", i+1, truncateString(t, 70))
		}
	}
	fmt.Println()

	// Step 3: Run 3 scan agents in parallel, guided by threat model
	if hub != nil {
		hub.SetPhase("scan", "Investigating threats")
	}
	display.PrintHeader("STEP 3: SECURITY REVIEW")
	display.PrintStatus("Running 3 reviewers guided by threat model...")
	fmt.Println()

	prompts := scan.PRScanPrompts(prInfo, threatModel)
	agents := DistributeAgents(3)
	names := []string{"Threat Investigation", "Data Flow Analysis", "Context Review"}

	var results []agent.AgentResult
	if hub != nil {
		results = agent.StreamMultipleWithWeb(agents, prompts, names, hub)
	} else {
		results = agent.StreamMultipleWithStatus(agents, prompts, names)
	}

	fmt.Println()
	display.PrintSuccess("Review complete")

	// Extract findings content
	findings := make([]string, len(results))
	for i, r := range results {
		findings[i] = r.Content
	}

	// Step 4: Synthesize report
	if hub != nil {
		hub.SetPhase("synthesize", "Synthesizing report")
	}
	display.PrintHeader("STEP 4: SYNTHESIZE")

	synthesisPrompt := scan.PRSynthesisPrompt(prInfo, threatModel, findings)
	var report string
	if hub != nil {
		report = agent.StreamSilentWithWeb(CreateAgent(), synthesisPrompt, "Synthesizing final report", hub)
	} else {
		report = agent.StreamSilent(CreateAgent(), synthesisPrompt, "Synthesizing final report")
	}

	fmt.Println()

	// Output report
	display.PrintHeader("SECURITY REVIEW REPORT")
	fmt.Println(report)

	// Create gist if requested
	if scanCreateGist {
		fmt.Println()
		display.PrintStatus("Creating secret gist...")
		gistURL, err := createScanGist(report, prInfo.Title)
		if err != nil {
			display.PrintError("Failed to create gist: %v", err)
		} else {
			display.PrintSuccess("Gist: %s", gistURL)
		}
	}

	// Keep web server running if dashboard is open
	if scanUseWeb {
		fmt.Println()
		display.PrintStatus("Dashboard still running. Press Ctrl+C to exit.")
		select {}
	}

	return nil
}

// loadPRInfo fetches PR metadata and diff from GitHub
func loadPRInfo(prURL string) (*scan.PRInfo, error) {
	// Parse the URL
	parts := strings.Split(strings.TrimPrefix(prURL, "https://github.com/"), "/")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid PR URL format")
	}

	owner := parts[0]
	repo := parts[1]
	number := parts[3]

	// Get PR metadata
	metaCmd := exec.Command("gh", "pr", "view", number, "--repo", owner+"/"+repo,
		"--json", "title,body,author,baseRefName,files",
		"--jq", `{title: .title, body: .body, author: .author.login, base: .baseRefName, files: [.files[].path]}`)

	metaOutput, err := metaCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get PR metadata: %w", err)
	}

	// Parse metadata (simple JSON parsing)
	prInfo := &scan.PRInfo{}
	metaStr := string(metaOutput)

	// Extract fields from JSON (simple extraction since we control the jq output)
	prInfo.Title = extractJSONString(metaStr, "title")
	prInfo.Description = extractJSONString(metaStr, "body")
	prInfo.Author = extractJSONString(metaStr, "author")
	prInfo.BaseBranch = extractJSONString(metaStr, "base")
	prInfo.Files = extractJSONArray(metaStr, "files")

	// Get the diff
	diffCmd := exec.Command("gh", "pr", "diff", number, "--repo", owner+"/"+repo)
	diffOutput, err := diffCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get PR diff: %w", err)
	}
	prInfo.Diff = string(diffOutput)

	return prInfo, nil
}

// extractJSONString extracts a string value from JSON (simple implementation)
func extractJSONString(json, key string) string {
	pattern := fmt.Sprintf(`"%s":"`, key)
	idx := strings.Index(json, pattern)
	if idx == -1 {
		// Try without quotes for the key
		pattern = fmt.Sprintf(`"%s": "`, key)
		idx = strings.Index(json, pattern)
		if idx == -1 {
			return ""
		}
	}

	start := idx + len(pattern)
	end := start
	escaped := false
	for end < len(json) {
		if escaped {
			escaped = false
			end++
			continue
		}
		if json[end] == '\\' {
			escaped = true
			end++
			continue
		}
		if json[end] == '"' {
			break
		}
		end++
	}

	return json[start:end]
}

// extractJSONArray extracts a string array from JSON (simple implementation)
func extractJSONArray(json, key string) []string {
	pattern := fmt.Sprintf(`"%s":[`, key)
	idx := strings.Index(json, pattern)
	if idx == -1 {
		pattern = fmt.Sprintf(`"%s": [`, key)
		idx = strings.Index(json, pattern)
		if idx == -1 {
			return nil
		}
	}

	start := idx + len(pattern)
	end := strings.Index(json[start:], "]")
	if end == -1 {
		return nil
	}

	arrayStr := json[start : start+end]
	var result []string

	// Simple parsing of ["a","b","c"]
	inQuote := false
	current := ""
	for _, c := range arrayStr {
		if c == '"' {
			if inQuote {
				if current != "" {
					result = append(result, current)
				}
				current = ""
			}
			inQuote = !inQuote
		} else if inQuote {
			current += string(c)
		}
	}

	return result
}

// runVulnScan runs the vulnerability regression scan mode
func runVulnScan(input string, hub *web.Hub) error {
	display.PrintHeader("VULNERABILITY SCAN")
	display.PrintStatus("Providers: %s", AgentBackend())
	display.PrintStatus("Input: %s", input)
	fmt.Println()

	// Step 1: Load content
	if hub != nil {
		hub.SetPhase("load", "Loading vulnerability information")
	}
	display.PrintStatus("Step 1: Loading vulnerability information...")

	content, err := loadInput(input)
	if err != nil {
		return fmt.Errorf("failed to load input: %w", err)
	}
	display.PrintSuccess("Loaded %d bytes", len(content))
	fmt.Println()

	// Step 2: Analyze to extract profile
	if hub != nil {
		hub.SetPhase("analyze", "Analyzing vulnerability")
	}
	display.PrintHeader("STEP 2: ANALYZE")

	var profile *scan.VulnProfile
	if hub != nil {
		output := agent.StreamSilentWithWeb(CreateAgent(), buildAnalyzePrompt(content), "Analyzing vulnerability", hub)
		profile, err = parseProfileFromOutput(output, content)
	} else {
		profile, err = scan.Analyze(CreateAgent(), content)
	}
	if err != nil {
		return fmt.Errorf("failed to analyze vulnerability: %w", err)
	}

	display.PrintSuccess("Vulnerability Profile:")
	display.PrintStatus("  Title: %s", profile.Title)
	display.PrintStatus("  Type: %s", profile.Type)
	display.PrintStatus("  Pattern: %s", truncateString(profile.Pattern, 80))
	if len(profile.Files) > 0 {
		display.PrintStatus("  Files: %s", strings.Join(profile.Files, ", "))
	}
	fmt.Println()

	// Step 3: Run 3 scan agents in parallel
	if hub != nil {
		hub.SetPhase("scan", "Scanning codebase")
	}
	display.PrintHeader("STEP 3: SCAN")
	display.PrintStatus("Running 3 scan agents in parallel...")
	fmt.Println()

	prompts := scan.ScanPrompts(profile)
	agents := DistributeAgents(3)
	names := []string{"Regression Check", "Variant Scan", "Deep Analysis"}

	var results []agent.AgentResult
	if hub != nil {
		results = agent.StreamMultipleWithWeb(agents, prompts, names, hub)
	} else {
		results = agent.StreamMultipleWithStatus(agents, prompts, names)
	}

	fmt.Println()
	display.PrintSuccess("Scan complete")

	// Extract findings content
	findings := make([]string, len(results))
	for i, r := range results {
		findings[i] = r.Content
	}

	// Step 4: Synthesize report
	if hub != nil {
		hub.SetPhase("synthesize", "Synthesizing report")
	}
	display.PrintHeader("STEP 4: SYNTHESIZE")

	synthesisPrompt := scan.SynthesisPrompt(profile, findings)
	var report string
	if hub != nil {
		report = agent.StreamSilentWithWeb(CreateAgent(), synthesisPrompt, "Synthesizing final report", hub)
	} else {
		report = agent.StreamSilent(CreateAgent(), synthesisPrompt, "Synthesizing final report")
	}

	fmt.Println()

	// Output report
	display.PrintHeader("SCAN REPORT")
	fmt.Println(report)

	// Create gist if requested
	if scanCreateGist {
		fmt.Println()
		display.PrintStatus("Creating secret gist...")
		gistURL, err := createScanGist(report, profile.Title)
		if err != nil {
			display.PrintError("Failed to create gist: %v", err)
		} else {
			display.PrintSuccess("Gist: %s", gistURL)
		}
	}

	// Keep web server running if dashboard is open
	if scanUseWeb {
		fmt.Println()
		display.PrintStatus("Dashboard still running. Press Ctrl+C to exit.")
		select {}
	}

	return nil
}

// loadInput loads content from a GitHub URL or local file
func loadInput(input string) (string, error) {
	if strings.HasPrefix(input, "https://github.com/") {
		return loadGitHubContent(input)
	}

	// Treat as local file
	absPath, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

// loadGitHubContent fetches content from a GitHub issue or PR
func loadGitHubContent(url string) (string, error) {
	// Try using gh CLI first (handles auth)
	if ghContent, err := loadWithGH(url); err == nil {
		return ghContent, nil
	}

	// Fallback to direct API call
	return loadWithHTTP(url)
}

// loadWithGH uses the GitHub CLI to fetch content
func loadWithGH(url string) (string, error) {
	// Parse the URL to extract owner/repo/type/number
	// Format: https://github.com/owner/repo/issues/123 or /pull/456
	parts := strings.Split(strings.TrimPrefix(url, "https://github.com/"), "/")
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid GitHub URL format")
	}

	owner := parts[0]
	repo := parts[1]
	itemType := parts[2] // "issues" or "pull"
	number := parts[3]

	var cmd *exec.Cmd
	if itemType == "pull" {
		cmd = exec.Command("gh", "pr", "view", number, "--repo", owner+"/"+repo, "--json", "title,body,comments", "--jq", ".title + \"\\n\\n\" + .body + \"\\n\\n\" + ([.comments[].body] | join(\"\\n\\n\"))")
	} else {
		cmd = exec.Command("gh", "issue", "view", number, "--repo", owner+"/"+repo, "--json", "title,body,comments", "--jq", ".title + \"\\n\\n\" + .body + \"\\n\\n\" + ([.comments[].body] | join(\"\\n\\n\"))")
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh command failed: %w", err)
	}

	return string(output), nil
}

// loadWithHTTP fetches content via HTTP (limited without auth)
func loadWithHTTP(url string) (string, error) {
	// Convert GitHub URL to API URL
	// https://github.com/owner/repo/issues/123 -> https://api.github.com/repos/owner/repo/issues/123
	apiURL := strings.Replace(url, "github.com", "api.github.com/repos", 1)

	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}

// buildAnalyzePrompt creates the prompt for vulnerability analysis
func buildAnalyzePrompt(content string) string {
	return fmt.Sprintf(`You are analyzing a security vulnerability report to extract key information for scanning a codebase.

## Input Content
%s

## Your Task
Extract a structured vulnerability profile from the above content. Identify:
1. What type of vulnerability this is (e.g., SQL Injection, XSS, Command Injection, Path Traversal, Auth Bypass, etc.)
2. What code pattern or anti-pattern was vulnerable
3. Which files were originally affected (if mentioned)
4. How the fix was implemented (if mentioned)

Output your analysis in this exact format:

---TITLE---
<brief title describing the vulnerability>
---TYPE---
<vulnerability type>
---PATTERN---
<description of the vulnerable code pattern to search for>
---FILES---
<comma-separated list of affected files, or "unknown" if not specified>
---FIX---
<how it was fixed, or "unknown" if not specified>
---END---
`, content)
}

// parseProfileFromOutput extracts a VulnProfile from agent output
func parseProfileFromOutput(output, rawContent string) (*scan.VulnProfile, error) {
	profile := &scan.VulnProfile{
		RawContent: rawContent,
	}

	profile.Title = extractSection(output, "---TITLE---", "---TYPE---")
	profile.Type = extractSection(output, "---TYPE---", "---PATTERN---")
	profile.Pattern = extractSection(output, "---PATTERN---", "---FILES---")
	filesStr := extractSection(output, "---FILES---", "---FIX---")
	profile.FixApproach = extractSection(output, "---FIX---", "---END---")

	if filesStr != "" && filesStr != "unknown" {
		for _, f := range strings.Split(filesStr, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				profile.Files = append(profile.Files, f)
			}
		}
	}

	if profile.Type == "" && profile.Pattern == "" {
		return nil, fmt.Errorf("failed to extract vulnerability profile from content")
	}

	return profile, nil
}

// extractSection extracts content between two markers
func extractSection(output, startMarker, endMarker string) string {
	startIdx := strings.Index(output, startMarker)
	if startIdx == -1 {
		return ""
	}
	startIdx += len(startMarker)

	endIdx := strings.Index(output[startIdx:], endMarker)
	if endIdx == -1 {
		return strings.TrimSpace(output[startIdx:])
	}

	return strings.TrimSpace(output[startIdx : startIdx+endIdx])
}

// truncateString limits string length for display
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// createScanGist creates a secret gist for the scan report
func createScanGist(report, title string) (string, error) {
	// Create a temp file
	tmpFile, err := os.CreateTemp("", "conclave-scan-*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(report); err != nil {
		return "", fmt.Errorf("failed to write report: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command("gh", "gist", "create", "--desc",
		fmt.Sprintf("Conclave Vulnerability Scan: %s", title),
		tmpFile.Name())

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh command failed: %w\nOutput: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}
