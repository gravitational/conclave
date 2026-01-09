package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const ContextFileName = "CONCLAVE.md"

// RepoContext holds learned context about a repository
type RepoContext struct {
	// File path
	path string

	// Parsed sections
	Overview       string            `yaml:"overview,omitempty"`
	FalsePositives []FalsePositive   `yaml:"false_positives,omitempty"`
	FocusAreas     []string          `yaml:"focus_areas,omitempty"`
	IgnorePatterns []string          `yaml:"ignore_patterns,omitempty"`
	SubsystemNotes map[string]string `yaml:"subsystem_notes,omitempty"`
	Findings       []Finding         `yaml:"confirmed_findings,omitempty"`
}

// FalsePositive represents a known false positive pattern
type FalsePositive struct {
	Subsystem   string    `yaml:"subsystem"`
	Pattern     string    `yaml:"pattern"`
	Reason      string    `yaml:"reason"`
	DateAdded   time.Time `yaml:"date_added"`
}

// Finding represents a confirmed security finding
type Finding struct {
	Subsystem   string    `yaml:"subsystem"`
	Title       string    `yaml:"title"`
	Description string    `yaml:"description"`
	DateFound   time.Time `yaml:"date_found"`
	Status      string    `yaml:"status"` // confirmed, fixed, wontfix
}

// Load reads a CONCLAVE.md file from the given directory
func Load(dir string) (*RepoContext, error) {
	path := filepath.Join(dir, ContextFileName)
	ctx := &RepoContext{
		path:           path,
		SubsystemNotes: make(map[string]string),
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// No context file yet, return empty context
		return ctx, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read context file: %w", err)
	}

	// Parse the markdown file with YAML frontmatter
	content := string(data)
	if err := ctx.parse(content); err != nil {
		return nil, fmt.Errorf("failed to parse context file: %w", err)
	}

	return ctx, nil
}

// parse extracts structured data from the markdown content
func (c *RepoContext) parse(content string) error {
	// Check for YAML frontmatter
	if strings.HasPrefix(content, "---\n") {
		parts := strings.SplitN(content[4:], "\n---\n", 2)
		if len(parts) == 2 {
			if err := yaml.Unmarshal([]byte(parts[0]), c); err != nil {
				return err
			}
			content = parts[1]
		}
	}

	// Parse markdown sections for overview
	lines := strings.Split(content, "\n")
	var currentSection string
	var sectionContent strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			// Save previous section
			if currentSection == "Overview" {
				c.Overview = strings.TrimSpace(sectionContent.String())
			}
			currentSection = strings.TrimPrefix(line, "## ")
			sectionContent.Reset()
		} else if currentSection != "" {
			sectionContent.WriteString(line)
			sectionContent.WriteString("\n")
		}
	}

	// Save last section if it was Overview
	if currentSection == "Overview" {
		c.Overview = strings.TrimSpace(sectionContent.String())
	}

	return nil
}

// Save writes the context back to the CONCLAVE.md file
func (c *RepoContext) Save() error {
	content := c.render()
	return os.WriteFile(c.path, []byte(content), 0644)
}

// render generates the markdown content
func (c *RepoContext) render() string {
	var b strings.Builder

	// YAML frontmatter
	b.WriteString("---\n")
	frontmatter, _ := yaml.Marshal(struct {
		FalsePositives []FalsePositive   `yaml:"false_positives,omitempty"`
		FocusAreas     []string          `yaml:"focus_areas,omitempty"`
		IgnorePatterns []string          `yaml:"ignore_patterns,omitempty"`
		SubsystemNotes map[string]string `yaml:"subsystem_notes,omitempty"`
		Findings       []Finding         `yaml:"confirmed_findings,omitempty"`
	}{
		FalsePositives: c.FalsePositives,
		FocusAreas:     c.FocusAreas,
		IgnorePatterns: c.IgnorePatterns,
		SubsystemNotes: c.SubsystemNotes,
		Findings:       c.Findings,
	})
	b.Write(frontmatter)
	b.WriteString("---\n\n")

	// Title
	b.WriteString("# Conclave Context\n\n")
	b.WriteString("This file contains learned context about this repository for security auditing.\n")
	b.WriteString("It is automatically updated by `conclave feedback` and read during audits.\n\n")

	// Overview
	b.WriteString("## Overview\n\n")
	if c.Overview != "" {
		b.WriteString(c.Overview)
	} else {
		b.WriteString("_No overview yet. Run `conclave plan` to generate one._")
	}
	b.WriteString("\n\n")

	// False Positives
	b.WriteString("## Known False Positives\n\n")
	if len(c.FalsePositives) == 0 {
		b.WriteString("_None recorded yet._\n")
	} else {
		for _, fp := range c.FalsePositives {
			b.WriteString(fmt.Sprintf("### %s\n", fp.Subsystem))
			b.WriteString(fmt.Sprintf("- **Pattern:** %s\n", fp.Pattern))
			b.WriteString(fmt.Sprintf("- **Reason:** %s\n", fp.Reason))
			b.WriteString(fmt.Sprintf("- **Added:** %s\n\n", fp.DateAdded.Format("2006-01-02")))
		}
	}
	b.WriteString("\n")

	// Focus Areas
	b.WriteString("## Focus Areas\n\n")
	if len(c.FocusAreas) == 0 {
		b.WriteString("_No specific focus areas defined._\n")
	} else {
		for _, area := range c.FocusAreas {
			b.WriteString(fmt.Sprintf("- %s\n", area))
		}
	}
	b.WriteString("\n")

	// Ignore Patterns
	b.WriteString("## Ignore Patterns\n\n")
	if len(c.IgnorePatterns) == 0 {
		b.WriteString("_No ignore patterns defined._\n")
	} else {
		for _, pattern := range c.IgnorePatterns {
			b.WriteString(fmt.Sprintf("- %s\n", pattern))
		}
	}
	b.WriteString("\n")

	// Subsystem Notes
	b.WriteString("## Subsystem Notes\n\n")
	if len(c.SubsystemNotes) == 0 {
		b.WriteString("_No subsystem-specific notes yet._\n")
	} else {
		for subsystem, notes := range c.SubsystemNotes {
			b.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", subsystem, notes))
		}
	}

	// Confirmed Findings
	b.WriteString("## Confirmed Findings\n\n")
	if len(c.Findings) == 0 {
		b.WriteString("_No confirmed findings recorded._\n")
	} else {
		for _, f := range c.Findings {
			b.WriteString(fmt.Sprintf("### %s (%s)\n", f.Title, f.Status))
			b.WriteString(fmt.Sprintf("- **Subsystem:** %s\n", f.Subsystem))
			b.WriteString(fmt.Sprintf("- **Found:** %s\n", f.DateFound.Format("2006-01-02")))
			b.WriteString(fmt.Sprintf("- %s\n\n", f.Description))
		}
	}

	return b.String()
}

// AddFalsePositive records a false positive pattern
func (c *RepoContext) AddFalsePositive(subsystem, pattern, reason string) {
	c.FalsePositives = append(c.FalsePositives, FalsePositive{
		Subsystem: subsystem,
		Pattern:   pattern,
		Reason:    reason,
		DateAdded: time.Now(),
	})
}

// AddSubsystemNote adds or updates a note for a subsystem
func (c *RepoContext) AddSubsystemNote(subsystem, note string) {
	if c.SubsystemNotes == nil {
		c.SubsystemNotes = make(map[string]string)
	}
	if existing, ok := c.SubsystemNotes[subsystem]; ok {
		c.SubsystemNotes[subsystem] = existing + "\n\n" + note
	} else {
		c.SubsystemNotes[subsystem] = note
	}
}

// AddFinding records a confirmed finding
func (c *RepoContext) AddFinding(subsystem, title, description, status string) {
	c.Findings = append(c.Findings, Finding{
		Subsystem:   subsystem,
		Title:       title,
		Description: description,
		DateFound:   time.Now(),
		Status:      status,
	})
}

// AddFocusArea adds a focus area
func (c *RepoContext) AddFocusArea(area string) {
	c.FocusAreas = append(c.FocusAreas, area)
}

// AddIgnorePattern adds an ignore pattern
func (c *RepoContext) AddIgnorePattern(pattern string) {
	c.IgnorePatterns = append(c.IgnorePatterns, pattern)
}

// SetOverview sets the overview text
func (c *RepoContext) SetOverview(overview string) {
	c.Overview = overview
}

// ForPrompt generates a text summary suitable for including in agent prompts
func (c *RepoContext) ForPrompt() string {
	if c.isEmpty() {
		return ""
	}

	var b strings.Builder
	b.WriteString("=== REPOSITORY CONTEXT (from previous audits) ===\n\n")

	if c.Overview != "" {
		b.WriteString("OVERVIEW:\n")
		b.WriteString(c.Overview)
		b.WriteString("\n\n")
	}

	if len(c.FalsePositives) > 0 {
		b.WriteString("KNOWN FALSE POSITIVES (do not report these):\n")
		for _, fp := range c.FalsePositives {
			b.WriteString(fmt.Sprintf("- [%s] %s - %s\n", fp.Subsystem, fp.Pattern, fp.Reason))
		}
		b.WriteString("\n")
	}

	if len(c.FocusAreas) > 0 {
		b.WriteString("FOCUS AREAS (pay special attention):\n")
		for _, area := range c.FocusAreas {
			b.WriteString(fmt.Sprintf("- %s\n", area))
		}
		b.WriteString("\n")
	}

	if len(c.IgnorePatterns) > 0 {
		b.WriteString("IGNORE PATTERNS (skip these areas):\n")
		for _, pattern := range c.IgnorePatterns {
			b.WriteString(fmt.Sprintf("- %s\n", pattern))
		}
		b.WriteString("\n")
	}

	if len(c.SubsystemNotes) > 0 {
		b.WriteString("SUBSYSTEM-SPECIFIC NOTES:\n")
		for subsystem, notes := range c.SubsystemNotes {
			b.WriteString(fmt.Sprintf("[%s]: %s\n", subsystem, notes))
		}
		b.WriteString("\n")
	}

	if len(c.Findings) > 0 {
		b.WriteString("PREVIOUSLY CONFIRMED FINDINGS (already known, don't re-report unless status changed):\n")
		for _, f := range c.Findings {
			b.WriteString(fmt.Sprintf("- [%s] %s (%s): %s\n", f.Subsystem, f.Title, f.Status, f.Description))
		}
		b.WriteString("\n")
	}

	b.WriteString("=== END REPOSITORY CONTEXT ===\n")
	return b.String()
}

// ForSubsystemPrompt generates context specific to a subsystem
func (c *RepoContext) ForSubsystemPrompt(subsystem string) string {
	if c.isEmpty() {
		return ""
	}

	var b strings.Builder
	var hasContent bool

	// Filter false positives for this subsystem
	var fps []FalsePositive
	for _, fp := range c.FalsePositives {
		if fp.Subsystem == subsystem || fp.Subsystem == "*" {
			fps = append(fps, fp)
		}
	}

	if len(fps) > 0 {
		hasContent = true
		b.WriteString("KNOWN FALSE POSITIVES FOR THIS SUBSYSTEM (do not report):\n")
		for _, fp := range fps {
			b.WriteString(fmt.Sprintf("- %s - %s\n", fp.Pattern, fp.Reason))
		}
		b.WriteString("\n")
	}

	// Subsystem-specific notes
	if notes, ok := c.SubsystemNotes[subsystem]; ok {
		hasContent = true
		b.WriteString("NOTES FOR THIS SUBSYSTEM:\n")
		b.WriteString(notes)
		b.WriteString("\n\n")
	}

	// Previous findings for this subsystem
	var findings []Finding
	for _, f := range c.Findings {
		if f.Subsystem == subsystem {
			findings = append(findings, f)
		}
	}

	if len(findings) > 0 {
		hasContent = true
		b.WriteString("PREVIOUS FINDINGS FOR THIS SUBSYSTEM:\n")
		for _, f := range findings {
			b.WriteString(fmt.Sprintf("- %s (%s): %s\n", f.Title, f.Status, f.Description))
		}
		b.WriteString("\n")
	}

	if !hasContent {
		return ""
	}

	return "=== SUBSYSTEM CONTEXT ===\n" + b.String() + "=== END SUBSYSTEM CONTEXT ===\n"
}

func (c *RepoContext) isEmpty() bool {
	return c.Overview == "" &&
		len(c.FalsePositives) == 0 &&
		len(c.FocusAreas) == 0 &&
		len(c.IgnorePatterns) == 0 &&
		len(c.SubsystemNotes) == 0 &&
		len(c.Findings) == 0
}

// Exists returns true if a CONCLAVE.md file exists
func (c *RepoContext) Exists() bool {
	_, err := os.Stat(c.path)
	return err == nil
}

// Path returns the path to the context file
func (c *RepoContext) Path() string {
	return c.path
}
