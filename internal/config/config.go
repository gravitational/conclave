package config

// ModelSpec represents a model specification parsed from config
type ModelSpec struct {
	Provider string // "claude", "codex", "gemini"
	Model    string // e.g., "sonnet", "gpt-5.2-pro", "sonnet[1m]"
	Effort   string // codex reasoning effort (low, medium, high, xhigh)
}

// IsEmpty returns true if the spec has no provider set
func (m ModelSpec) IsEmpty() bool {
	return m.Provider == ""
}

// ConveneConfig holds the model specs for each convene sub-phase
type ConveneConfig struct {
	SteelMan ModelSpec
	Critique ModelSpec
	Judge    ModelSpec
}

// StageConfig holds model specs for each stage of the pipeline
type StageConfig struct {
	Plan     ModelSpec   // Single agent for planning
	Assess   []ModelSpec // List - length determines agent count
	Convene  ConveneConfig
	Complete ModelSpec // Single agent for final synthesis
}

// Profile represents a named configuration profile
type Profile struct {
	Name   string
	Stages StageConfig
}

// Config represents the full configuration file
type Config struct {
	Instructions []string
	Profiles     map[string]Profile
}

// NewConfig creates an empty Config
func NewConfig() *Config {
	return &Config{
		Profiles: make(map[string]Profile),
	}
}

// GetProfile returns a profile by name, or nil if not found
func (c *Config) GetProfile(name string) *Profile {
	if c == nil || c.Profiles == nil {
		return nil
	}
	profile, ok := c.Profiles[name]
	if !ok {
		return nil
	}
	return &profile
}

// ProfileNames returns a list of all available profile names
func (c *Config) ProfileNames() []string {
	if c == nil || c.Profiles == nil {
		return nil
	}
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	return names
}
