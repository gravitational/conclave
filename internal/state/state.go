package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// State manages the .conclave directory and all persisted data
type State struct {
	root string // Path to the codebase root
	dir  string // Path to .conclave directory
}

// Plan represents an analysis plan
type Plan struct {
	ID           string      `yaml:"id"`
	Name         string      `yaml:"name"`
	Created      time.Time   `yaml:"created"`
	CodebaseRoot string      `yaml:"codebase_root"`
	Agent        string      `yaml:"agent"`
	Overview     string      `yaml:"-"`
	Subsystems   []Subsystem `yaml:"-"`
}

// Subsystem represents a part of the codebase to analyze
type Subsystem struct {
	Slug         string `yaml:"slug"`
	Name         string `yaml:"name"`
	Paths        string `yaml:"paths"`
	Description  string `yaml:"description"`
	Interactions string `yaml:"interactions"`
}

// AgentMeta captures the agent that produced a piece of content
type AgentMeta struct {
	Provider string `yaml:"provider"`        // "codex", "claude", "gemini"
	Model    string `yaml:"model,omitempty"` // e.g., "o3", "sonnet", "gemini-2.5-pro"
}

// Perspective represents a single agent's assessment of a subsystem
type Perspective struct {
	AgentNum int       `yaml:"agent_num"`
	Agent    AgentMeta `yaml:"agent"`
	Content  string    `yaml:"-"` // Stored in body, not frontmatter
}

// FormatLabel returns a human-readable label like "Agent 1 (Claude/sonnet)"
func (p *Perspective) FormatLabel() string {
	if p.Agent.Model != "" {
		return fmt.Sprintf("Agent %d (%s/%s)", p.AgentNum, capitalize(p.Agent.Provider), p.Agent.Model)
	}
	if p.Agent.Provider != "" && p.Agent.Provider != "unknown" {
		return fmt.Sprintf("Agent %d (%s)", p.AgentNum, capitalize(p.Agent.Provider))
	}
	return fmt.Sprintf("Agent %d", p.AgentNum)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Slug returns a URL-safe version of the plan name
func (p *Plan) Slug() string {
	slug := strings.ToLower(p.Name)
	slug = strings.ReplaceAll(slug, " ", "-")
	// Remove any characters that aren't alphanumeric or hyphens
	var clean strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			clean.WriteRune(r)
		}
	}
	return clean.String()
}

// New creates a new State for the given codebase root
func New(root string) (*State, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(absRoot, ".conclave")

	// Create directory structure
	dirs := []string{
		dir,
		filepath.Join(dir, "plans"),
		filepath.Join(dir, "assessments"),
		filepath.Join(dir, "debates"),
		filepath.Join(dir, "results"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}

	return &State{
		root: absRoot,
		dir:  dir,
	}, nil
}

// PlanPath returns the path for a plan file
func (s *State) PlanPath(id, slug string) string {
	filename := fmt.Sprintf("%s-%s.md", id[:8], slug)
	return filepath.Join(s.dir, "plans", filename)
}

// SavePlan saves a plan to disk
func (s *State) SavePlan(p *Plan) (string, error) {
	path := s.PlanPath(p.ID, p.Slug())

	// Build frontmatter
	frontmatter := struct {
		ID           string    `yaml:"id"`
		Name         string    `yaml:"name"`
		Created      time.Time `yaml:"created"`
		CodebaseRoot string    `yaml:"codebase_root"`
		Agent        string    `yaml:"agent"`
	}{
		ID:           p.ID,
		Name:         p.Name,
		Created:      p.Created,
		CodebaseRoot: p.CodebaseRoot,
		Agent:        p.Agent,
	}

	fm, err := yaml.Marshal(frontmatter)
	if err != nil {
		return "", err
	}

	// Build content
	var content strings.Builder
	content.WriteString("---\n")
	content.Write(fm)
	content.WriteString("---\n\n")
	content.WriteString("# Codebase Analysis: ")
	content.WriteString(p.Name)
	content.WriteString("\n\n## Overview\n\n")
	content.WriteString(p.Overview)
	content.WriteString("\n\n## Subsystems\n")

	for _, sub := range p.Subsystems {
		content.WriteString(fmt.Sprintf("\n### %s\n", sub.Slug))
		content.WriteString(fmt.Sprintf("**Name:** %s\n", sub.Name))
		content.WriteString(fmt.Sprintf("**Paths:** %s\n", sub.Paths))
		content.WriteString(fmt.Sprintf("**Description:** %s\n", sub.Description))
		content.WriteString(fmt.Sprintf("**Interactions:** %s\n", sub.Interactions))
	}

	if err := os.WriteFile(path, []byte(content.String()), 0644); err != nil {
		return "", err
	}

	return path, nil
}

// LoadPlanByID loads a plan by its UUID
func (s *State) LoadPlanByID(id string) (*Plan, error) {
	plans, err := s.ListPlans()
	if err != nil {
		return nil, err
	}

	for _, p := range plans {
		if strings.HasPrefix(p.ID, id) {
			return p, nil
		}
	}

	return nil, fmt.Errorf("plan not found: %s", id)
}

// LoadMostRecentPlan loads the most recently created plan
func (s *State) LoadMostRecentPlan() (*Plan, error) {
	plans, err := s.ListPlans()
	if err != nil {
		return nil, err
	}

	if len(plans) == 0 {
		return nil, fmt.Errorf("no plans found - run 'conclave plan' first")
	}

	// Sort by created time descending
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].Created.After(plans[j].Created)
	})

	return plans[0], nil
}

// ListPlans returns all plans
func (s *State) ListPlans() ([]*Plan, error) {
	plansDir := filepath.Join(s.dir, "plans")
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var plans []*Plan
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(plansDir, entry.Name())
		plan, err := s.loadPlanFromFile(path)
		if err != nil {
			continue // Skip invalid files
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func (s *State) loadPlanFromFile(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)

	// Parse frontmatter
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("invalid plan file: missing frontmatter")
	}

	endIdx := strings.Index(content[4:], "\n---\n")
	if endIdx == -1 {
		return nil, fmt.Errorf("invalid plan file: unclosed frontmatter")
	}

	fmContent := content[4 : 4+endIdx]
	body := content[4+endIdx+5:]

	var plan Plan
	if err := yaml.Unmarshal([]byte(fmContent), &plan); err != nil {
		return nil, err
	}

	// Parse body for overview and subsystems
	plan.Overview, plan.Subsystems = parsePlanBody(body)

	return &plan, nil
}

func parsePlanBody(body string) (string, []Subsystem) {
	var overview string
	var subsystems []Subsystem

	lines := strings.Split(body, "\n")
	var currentSection string
	var currentSubsystem *Subsystem
	var overviewLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "## Overview") {
			currentSection = "overview"
			continue
		}
		if strings.HasPrefix(line, "## Subsystems") {
			currentSection = "subsystems"
			continue
		}
		if strings.HasPrefix(line, "### ") && currentSection == "subsystems" {
			// Save previous subsystem
			if currentSubsystem != nil {
				subsystems = append(subsystems, *currentSubsystem)
			}
			slug := strings.TrimPrefix(line, "### ")
			currentSubsystem = &Subsystem{Slug: strings.TrimSpace(slug)}
			continue
		}

		if currentSection == "overview" {
			overviewLines = append(overviewLines, line)
		} else if currentSubsystem != nil {
			if strings.HasPrefix(line, "**Name:**") {
				currentSubsystem.Name = strings.TrimSpace(strings.TrimPrefix(line, "**Name:**"))
			} else if strings.HasPrefix(line, "**Paths:**") {
				currentSubsystem.Paths = strings.TrimSpace(strings.TrimPrefix(line, "**Paths:**"))
			} else if strings.HasPrefix(line, "**Description:**") {
				currentSubsystem.Description = strings.TrimSpace(strings.TrimPrefix(line, "**Description:**"))
			} else if strings.HasPrefix(line, "**Interactions:**") {
				currentSubsystem.Interactions = strings.TrimSpace(strings.TrimPrefix(line, "**Interactions:**"))
			}
		}
	}

	// Save last subsystem
	if currentSubsystem != nil {
		subsystems = append(subsystems, *currentSubsystem)
	}

	overview = strings.TrimSpace(strings.Join(overviewLines, "\n"))

	return overview, subsystems
}

// GenerateID creates a new UUID
func GenerateID() string {
	return uuid.New().String()
}

// SavePerspective saves an agent perspective with metadata
func (s *State) SavePerspective(planID, subsystem string, agentNum int, agent AgentMeta, content string) (string, error) {
	dir := filepath.Join(s.dir, "assessments", planID[:8], subsystem)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	// Build frontmatter
	frontmatter := struct {
		AgentNum int    `yaml:"agent_num"`
		Provider string `yaml:"provider"`
		Model    string `yaml:"model,omitempty"`
	}{
		AgentNum: agentNum,
		Provider: agent.Provider,
		Model:    agent.Model,
	}

	fm, err := yaml.Marshal(frontmatter)
	if err != nil {
		return "", err
	}

	// Build content with frontmatter
	var output strings.Builder
	output.WriteString("---\n")
	output.Write(fm)
	output.WriteString("---\n\n")
	output.WriteString(content)

	path := filepath.Join(dir, fmt.Sprintf("agent-%d.md", agentNum))
	if err := os.WriteFile(path, []byte(output.String()), 0644); err != nil {
		return "", err
	}

	return path, nil
}

// LoadPerspectives loads all perspectives for a subsystem
func (s *State) LoadPerspectives(planID, subsystem string) ([]Perspective, error) {
	dir := filepath.Join(s.dir, "assessments", planID[:8], subsystem)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var perspectives []Perspective
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		content := string(data)
		p := Perspective{}

		// Parse frontmatter if present (backwards compatibility)
		if strings.HasPrefix(content, "---\n") {
			endIdx := strings.Index(content[4:], "\n---\n")
			if endIdx != -1 {
				fmContent := content[4 : 4+endIdx]
				// Parse frontmatter into struct
				var fm struct {
					AgentNum int    `yaml:"agent_num"`
					Provider string `yaml:"provider"`
					Model    string `yaml:"model"`
				}
				if err := yaml.Unmarshal([]byte(fmContent), &fm); err == nil {
					p.AgentNum = fm.AgentNum
					p.Agent = AgentMeta{Provider: fm.Provider, Model: fm.Model}
				}
				p.Content = strings.TrimPrefix(content[4+endIdx+5:], "\n")
			} else {
				p.Content = content // No valid frontmatter
			}
		} else {
			// Legacy file without frontmatter
			p.Content = content
			// Extract agent number from filename (agent-1.md -> 1)
			name := entry.Name()
			if len(name) > 9 && strings.HasPrefix(name, "agent-") {
				numStr := name[6 : len(name)-3]
				fmt.Sscanf(numStr, "%d", &p.AgentNum)
			}
			p.Agent = AgentMeta{Provider: "unknown"}
		}

		perspectives = append(perspectives, p)
	}

	// Sort by agent number
	sort.Slice(perspectives, func(i, j int) bool {
		return perspectives[i].AgentNum < perspectives[j].AgentNum
	})

	return perspectives, nil
}

// SaveDebate saves a debate output
func (s *State) SaveDebate(planID, subsystem string, debateNum int, content string) (string, error) {
	dir := filepath.Join(s.dir, "debates", planID[:8], subsystem)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, fmt.Sprintf("debate-%d.md", debateNum))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}

	return path, nil
}

// LoadDebates loads all debate outputs for a subsystem
func (s *State) LoadDebates(planID, subsystem string) ([]string, error) {
	dir := filepath.Join(s.dir, "debates", planID[:8], subsystem)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var debates []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		debates = append(debates, string(data))
	}

	return debates, nil
}

// SaveResult saves a final result
func (s *State) SaveResult(planID, subsystem string, content string) (string, error) {
	dir := filepath.Join(s.dir, "results", planID[:8])
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, fmt.Sprintf("%s.md", subsystem))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}

	return path, nil
}

// LoadResult loads a final result
func (s *State) LoadResult(planID, subsystem string) (string, error) {
	path := filepath.Join(s.dir, "results", planID[:8], fmt.Sprintf("%s.md", subsystem))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}
