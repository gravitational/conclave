package scan

import (
	"fmt"
	"strings"

	"github.com/rob-picard-teleport/conclave/internal/agent"
)

// VulnProfile represents the extracted vulnerability information
type VulnProfile struct {
	Title       string   // Brief title of the vulnerability
	Type        string   // Vulnerability type (SQL Injection, XSS, etc.)
	Pattern     string   // What code pattern to look for
	Files       []string // Originally affected files
	FixApproach string   // How it was fixed
	RawContent  string   // Original content for reference
}

// Analyze extracts a vulnerability profile from issue/PR content
func Analyze(ag agent.Agent, content string) (*VulnProfile, error) {
	prompt := fmt.Sprintf(`You are analyzing a security vulnerability report to extract key information for scanning a codebase.

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

	output, err := agent.RunAndCollect(ag, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze content: %w", err)
	}

	return parseProfile(output, content)
}

// parseProfile extracts the structured profile from agent output
func parseProfile(output, rawContent string) (*VulnProfile, error) {
	profile := &VulnProfile{
		RawContent: rawContent,
	}

	// Extract each section
	profile.Title = extractSection(output, "---TITLE---", "---TYPE---")
	profile.Type = extractSection(output, "---TYPE---", "---PATTERN---")
	profile.Pattern = extractSection(output, "---PATTERN---", "---FILES---")
	filesStr := extractSection(output, "---FILES---", "---FIX---")
	profile.FixApproach = extractSection(output, "---FIX---", "---END---")

	// Parse files list
	if filesStr != "" && filesStr != "unknown" {
		for _, f := range strings.Split(filesStr, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				profile.Files = append(profile.Files, f)
			}
		}
	}

	// Validate we got at least the essentials
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

// ScanPrompts generates prompts for the 3 parallel scanning agents
func ScanPrompts(profile *VulnProfile) []string {
	baseContext := fmt.Sprintf(`## Vulnerability Context
**Title:** %s
**Type:** %s
**Vulnerable Pattern:** %s
**Originally Affected Files:** %s
**Fix Approach:** %s

## Original Report
%s`,
		profile.Title,
		profile.Type,
		profile.Pattern,
		formatFiles(profile.Files),
		profile.FixApproach,
		truncate(profile.RawContent, 2000),
	)

	// Three different scanning perspectives
	prompts := []string{
		// Agent 1: Regression check - look for the original vulnerability returning
		fmt.Sprintf(`You are a security researcher checking for vulnerability regressions.

%s

## Your Task
Check if the ORIGINAL vulnerability has regressed (been reintroduced) in the codebase.

Focus on:
1. The originally affected files (if known): %s
2. The specific vulnerable pattern that was fixed
3. Any new code that might have reintroduced the same issue

Search the codebase thoroughly. If the original files are known, start there. Look for:
- The same vulnerable pattern in the same locations
- Similar code that might have been copy-pasted elsewhere
- Refactored code that reintroduced the vulnerability

Report your findings in this format:

## Regression Check Results

### Status: [REGRESSION FOUND / NO REGRESSION / UNCERTAIN]

### Analysis
<your detailed analysis>

### Evidence
<specific code locations and snippets if regression found>

### Recommendation
<what should be done>
`, baseContext, formatFiles(profile.Files)),

		// Agent 2: Pattern variants - look for similar patterns elsewhere
		fmt.Sprintf(`You are a security researcher looking for variant vulnerabilities.

%s

## Your Task
Search the ENTIRE codebase for SIMILAR vulnerable patterns that might exist elsewhere.

The original vulnerability was of type "%s" with pattern: %s

Look for:
1. Similar code patterns in different parts of the codebase
2. The same anti-pattern applied to different data or contexts
3. Partial fixes where some instances were missed
4. Related vulnerability classes that might exist

Cast a wide net - check all relevant files, not just the originally affected ones.

Report your findings in this format:

## Variant Scan Results

### Variants Found: [number]

### Finding 1 (if any)
**Location:** <file:line>
**Pattern:** <what vulnerable pattern was found>
**Similarity:** <how it relates to the original vulnerability>
**Severity:** <Critical/High/Medium/Low>
**Evidence:** <code snippet>

### Finding 2 (if any)
...

### Summary
<overall assessment of variant risk in the codebase>
`, baseContext, profile.Type, profile.Pattern),

		// Agent 3: Deep dive - comprehensive analysis
		fmt.Sprintf(`You are a senior security researcher conducting a comprehensive vulnerability analysis.

%s

## Your Task
Perform a DEEP security analysis related to this vulnerability class.

Consider:
1. **Attack Surface:** What other entry points could be affected by this vulnerability type?
2. **Data Flow:** Trace how untrusted data flows through the application - are there other sinks?
3. **Defense in Depth:** Are there other layers of protection that should exist?
4. **Related Issues:** What other security issues might exist in the same code areas?

Think like an attacker. If you were trying to exploit "%s" vulnerabilities in this codebase, where would you look?

Report your findings in this format:

## Deep Analysis Results

### Attack Surface Assessment
<analysis of potential entry points>

### Data Flow Analysis
<how untrusted data flows and where it might be vulnerable>

### Additional Findings
<any other security issues discovered>

### Recommendations
<prioritized list of security improvements>

### Risk Summary
<overall risk assessment for this vulnerability class>
`, baseContext, profile.Type),
	}

	return prompts
}

// SynthesisPrompt generates the prompt for the final report
func SynthesisPrompt(profile *VulnProfile, findings []string) string {
	findingsSection := ""
	for i, f := range findings {
		findingsSection += fmt.Sprintf("\n## Agent %d Findings\n%s\n", i+1, f)
	}

	return fmt.Sprintf(`You are synthesizing security scan results into a final report.

## Original Vulnerability
**Title:** %s
**Type:** %s
**Pattern:** %s

## Scan Results
%s

## Your Task
Create a concise, actionable security report that:

1. **Summarizes the scan results** - Did we find regressions? Variants? Related issues?
2. **Prioritizes findings** - What needs immediate attention?
3. **Provides clear remediation** - Specific steps to fix each issue
4. **Assesses overall risk** - How secure is this codebase against this vulnerability class?

Format your report as:

# Vulnerability Scan Report: %s

## Executive Summary
<2-3 sentence summary of findings>

## Findings

### Critical
<any critical issues requiring immediate attention>

### High
<high severity issues>

### Medium
<medium severity issues>

### Low
<low severity issues or recommendations>

## Remediation Plan
<ordered list of steps to address findings>

## Conclusion
<final risk assessment and recommendations>
`, profile.Title, profile.Type, profile.Pattern, findingsSection, profile.Title)
}

// formatFiles formats the files list for display
func formatFiles(files []string) string {
	if len(files) == 0 {
		return "unknown"
	}
	return strings.Join(files, ", ")
}

// truncate limits string length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// PRInfo contains metadata about a pull request
type PRInfo struct {
	Title       string
	Description string
	Author      string
	BaseBranch  string
	Files       []string // Changed files
	Diff        string   // The actual diff content
}

// ThreatModel contains the security-focused analysis of a PR
type ThreatModel struct {
	Summary       string   // What the PR does from a security perspective
	Components    []string // Security-relevant components being modified
	DataFlows     []string // How data moves through the changes
	TrustBoundary string   // Trust boundaries being crossed or modified
	Threats       []string // Specific threats to investigate
	AttackSurface string   // What attack surface is affected
	RawOutput     string   // Full threat model output for reference
}

// ThreatModelPrompt generates the prompt to threat model a PR
func ThreatModelPrompt(pr *PRInfo) string {
	return fmt.Sprintf(`You are a security architect performing rapid threat modeling on a pull request.

## Pull Request
**Title:** %s
**Author:** %s
**Base Branch:** %s
**Changed Files:** %s

## Description
%s

## Diff
%s

## Your Task
Perform a concise, technical threat model of this PR. Focus on understanding:
1. What security-relevant changes are being made
2. What could go wrong from a security perspective
3. What specific threats reviewers should investigate

Be technical and specific. Skip boilerplate - focus on what matters for THIS PR.

Output in this exact format:

---SUMMARY---
<1-2 sentences: what this PR does from a security perspective>
---COMPONENTS---
<bullet list of security-relevant components being modified, e.g., "auth middleware", "SQL query builder", "file upload handler">
---DATAFLOWS---
<bullet list of data flows to trace, e.g., "user input → JSON parser → database query", "file upload → disk storage → served to users">
---TRUSTBOUNDARY---
<describe any trust boundaries being crossed or modified, or "none" if N/A>
---THREATS---
<numbered list of specific threats to investigate, e.g., "1. SQL injection in new search query", "2. Path traversal in file upload destination">
---ATTACKSURFACE---
<what attack surface is added/modified/removed>
---END---
`, pr.Title, pr.Author, pr.BaseBranch, strings.Join(pr.Files, ", "), pr.Description, truncate(pr.Diff, 12000))
}

// ParseThreatModel extracts a ThreatModel from agent output
func ParseThreatModel(output string) *ThreatModel {
	tm := &ThreatModel{RawOutput: output}

	tm.Summary = extractSection(output, "---SUMMARY---", "---COMPONENTS---")
	tm.Components = parseLines(extractSection(output, "---COMPONENTS---", "---DATAFLOWS---"))
	tm.DataFlows = parseLines(extractSection(output, "---DATAFLOWS---", "---TRUSTBOUNDARY---"))
	tm.TrustBoundary = extractSection(output, "---TRUSTBOUNDARY---", "---THREATS---")
	tm.Threats = parseLines(extractSection(output, "---THREATS---", "---ATTACKSURFACE---"))
	tm.AttackSurface = extractSection(output, "---ATTACKSURFACE---", "---END---")

	return tm
}

// parseLines splits text into lines, filtering empty ones
func parseLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		// Remove bullet points and numbering
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if len(line) > 2 && line[0] >= '0' && line[0] <= '9' && (line[1] == '.' || line[1] == ')') {
			line = strings.TrimSpace(line[2:])
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// FormatForDisplay returns a concise display of the threat model
func (tm *ThreatModel) FormatForDisplay() string {
	var sb strings.Builder
	sb.WriteString(tm.Summary)
	if len(tm.Threats) > 0 {
		sb.WriteString("\n  Threats: ")
		if len(tm.Threats) <= 3 {
			sb.WriteString(strings.Join(tm.Threats, "; "))
		} else {
			sb.WriteString(fmt.Sprintf("%s; %s; +%d more", tm.Threats[0], tm.Threats[1], len(tm.Threats)-2))
		}
	}
	return sb.String()
}

// PRScanPrompts generates prompts for scanning PR changes, guided by threat model
func PRScanPrompts(pr *PRInfo, tm *ThreatModel) []string {
	// Build threat-focused context
	threatContext := ""
	if len(tm.Threats) > 0 {
		threatContext = "\n## Identified Threats to Investigate\n"
		for i, t := range tm.Threats {
			threatContext += fmt.Sprintf("%d. %s\n", i+1, t)
		}
	}

	dataFlowContext := ""
	if len(tm.DataFlows) > 0 {
		dataFlowContext = "\n## Data Flows to Trace\n"
		for _, df := range tm.DataFlows {
			dataFlowContext += fmt.Sprintf("- %s\n", df)
		}
	}

	baseContext := fmt.Sprintf(`## Pull Request
**Title:** %s
**Author:** %s
**Files:** %s

## Threat Model Summary
%s

## Trust Boundaries
%s

## Attack Surface
%s
%s%s
## Diff
%s`,
		pr.Title,
		pr.Author,
		strings.Join(pr.Files, ", "),
		tm.Summary,
		tm.TrustBoundary,
		tm.AttackSurface,
		threatContext,
		dataFlowContext,
		truncate(pr.Diff, 12000),
	)

	prompts := []string{
		// Agent 1: Investigate identified threats
		fmt.Sprintf(`You are a security engineer investigating specific threats in a pull request.

%s

## Your Task
Investigate each identified threat thoroughly. For each threat:
1. Determine if the vulnerability actually exists in the code
2. Assess exploitability - can an attacker actually trigger this?
3. Evaluate impact - what's the worst case?

Be rigorous: confirm or rule out each threat with evidence from the code.

Report your findings:

## Threat Investigation Results

### Threat 1: <threat description>
**Status:** CONFIRMED / RULED OUT / NEEDS MORE INVESTIGATION
**Evidence:** <specific code locations and why>
**Exploitability:** <how an attacker would exploit, or why they can't>
**Impact:** <what damage could result>
**Severity:** Critical/High/Medium/Low/N/A

### Threat 2: ...
(repeat for each identified threat)

### Additional Findings
<any other vulnerabilities discovered during investigation>

### Summary
<overall threat assessment>
`, baseContext),

		// Agent 2: Trace data flows
		fmt.Sprintf(`You are a security researcher tracing data flows through code changes.

%s

## Your Task
Trace each identified data flow end-to-end:
1. Where does the data originate? (user input, file, network, etc.)
2. What transformations/validations happen?
3. Where does it end up? (database, file system, response, etc.)

Look for:
- Missing input validation
- Insufficient sanitization before dangerous operations
- Data reaching sensitive sinks without proper checks

Report your findings:

## Data Flow Analysis

### Flow 1: <flow description>
**Source:** <where data comes from>
**Path:** <how it moves through the code>
**Sink:** <where it ends up>
**Validation:** <what checks exist, or "NONE">
**Risk:** <what could go wrong>

### Flow 2: ...
(repeat for each data flow)

### Trust Boundary Crossings
<analysis of data crossing trust boundaries>

### Recommendations
<specific fixes for any issues found>
`, baseContext),

		// Agent 3: Contextual analysis and missing controls
		fmt.Sprintf(`You are a security architect reviewing changes in context.

%s

## Your Task
Examine these changes in the broader codebase context:
1. Read the surrounding code to understand existing security patterns
2. Check if the new code follows those patterns
3. Identify any missing security controls

Use your tools to explore beyond the diff. Questions to answer:
- How does existing code handle similar operations?
- Are there security helpers/utilities that should be used here?
- What security controls exist elsewhere that are missing here?

Report your findings:

## Contextual Security Analysis

### Pattern Compliance
<does this code follow existing security patterns? specifics>

### Missing Controls
**Control:** <what's missing>
**Location:** <where it should be added>
**Rationale:** <why it's needed>
**Example:** <how similar code handles this elsewhere>

### Integration Risks
<security issues at boundaries with existing code>

### Recommendations
<prioritized list of changes needed>
`, baseContext),
	}

	return prompts
}

// PRScanPromptsBasic generates basic prompts without threat model (fallback)
func PRScanPromptsBasic(pr *PRInfo) []string {
	return PRScanPrompts(pr, &ThreatModel{
		Summary:       "Security review of PR changes",
		TrustBoundary: "Unknown - analyze the diff",
		AttackSurface: "Unknown - analyze the diff",
		Threats:       []string{"Review for common vulnerability patterns"},
		DataFlows:     []string{"Trace all user input through the changes"},
	})
}

// PRSynthesisPrompt generates the synthesis prompt for PR scan results
func PRSynthesisPrompt(pr *PRInfo, tm *ThreatModel, findings []string) string {
	findingsSection := ""
	for i, f := range findings {
		findingsSection += fmt.Sprintf("\n## Reviewer %d Findings\n%s\n", i+1, f)
	}

	threatSection := ""
	if len(tm.Threats) > 0 {
		threatSection = "\n## Identified Threats\n"
		for i, t := range tm.Threats {
			threatSection += fmt.Sprintf("%d. %s\n", i+1, t)
		}
	}

	return fmt.Sprintf(`You are synthesizing security review results for a pull request.

## Pull Request
**Title:** %s
**Author:** %s
**Changed Files:** %s

## Threat Model
%s
%s
## Review Results
%s

## Your Task
Create a concise, actionable security review report:

1. For each identified threat, state whether it was CONFIRMED, RULED OUT, or NEEDS ATTENTION
2. List any additional issues found beyond the threat model
3. Provide clear verdict and required changes

Format your report as:

# PR Security Review: %s

## Verdict: [APPROVE / NEEDS CHANGES / BLOCK]

## Executive Summary
<2-3 sentences: key findings and recommendation>

## Threat Assessment
<for each identified threat: status and one-line explanation>

## Confirmed Issues

### Critical
<critical issues or "None">

### High
<high issues or "None">

### Medium
<medium issues or "None">

### Low
<low issues or "None">

## Required Changes
<numbered list of specific changes needed, or "None - approve as-is">
`, pr.Title, pr.Author, strings.Join(pr.Files, ", "), tm.Summary, threatSection, findingsSection, pr.Title)
}
