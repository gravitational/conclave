package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath returns the default config file path (~/.conclave/config.yaml)
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".conclave", "config.yaml")
}

// Load loads the config from the default path
// Returns nil config with no error if the file doesn't exist
func Load() (*Config, error) {
	return LoadFrom(DefaultConfigPath())
}

// LoadFrom loads the config from the specified path
// Returns nil config with no error if the file doesn't exist
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	return Parse(data)
}

// rawConfig is the intermediate structure for YAML unmarshaling
type rawConfig struct {
	Profiles map[string]rawProfile `yaml:"profiles"`
}

type rawProfile struct {
	Plan     string         `yaml:"plan"`
	Assess   []string       `yaml:"assess"`
	Convene  rawConvene     `yaml:"convene"`
	Complete string         `yaml:"complete"`
}

type rawConvene struct {
	SteelMan string `yaml:"steelMan"`
	Critique string `yaml:"critique"`
	Judge    string `yaml:"judge"`
}

// Parse parses config data from YAML
func Parse(data []byte) (*Config, error) {
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	config := NewConfig()

	for name, rawProfile := range raw.Profiles {
		profile := Profile{
			Name: name,
			Stages: StageConfig{
				Plan:     ParseModelSpec(rawProfile.Plan),
				Complete: ParseModelSpec(rawProfile.Complete),
				Convene: ConveneConfig{
					SteelMan: ParseModelSpec(rawProfile.Convene.SteelMan),
					Critique: ParseModelSpec(rawProfile.Convene.Critique),
					Judge:    ParseModelSpec(rawProfile.Convene.Judge),
				},
			},
		}

		// Parse assess list
		for _, spec := range rawProfile.Assess {
			profile.Stages.Assess = append(profile.Stages.Assess, ParseModelSpec(spec))
		}

		// Validate provider names
		if err := validateProfile(&profile); err != nil {
			return nil, fmt.Errorf("invalid profile %q: %w", name, err)
		}

		config.Profiles[name] = profile
	}

	return config, nil
}

// validateProfile checks that all model specs use valid providers
func validateProfile(p *Profile) error {
	specs := []struct {
		name string
		spec ModelSpec
	}{
		{"plan", p.Stages.Plan},
		{"complete", p.Stages.Complete},
		{"convene.steelMan", p.Stages.Convene.SteelMan},
		{"convene.critique", p.Stages.Convene.Critique},
		{"convene.judge", p.Stages.Convene.Judge},
	}

	for _, s := range specs {
		if !s.spec.IsEmpty() && !IsValidProvider(s.spec.Provider) {
			return fmt.Errorf("%s: invalid provider %q (valid: %v)", s.name, s.spec.Provider, ValidProviderList())
		}
	}

	for i, spec := range p.Stages.Assess {
		if !spec.IsEmpty() && !IsValidProvider(spec.Provider) {
			return fmt.Errorf("assess[%d]: invalid provider %q (valid: %v)", i, spec.Provider, ValidProviderList())
		}
	}

	return nil
}
