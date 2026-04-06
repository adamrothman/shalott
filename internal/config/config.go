package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// Config is the top-level shalott configuration.
type Config struct {
	Mirrors []Mirror `yaml:"mirrors" validate:"required,min=1,dive"`
}

// Mirror defines a single source-to-target mirroring configuration.
type Mirror struct {
	Name         string        `yaml:"name" validate:"required"`
	Source       string        `yaml:"source" validate:"required"`
	Target       string        `yaml:"target" validate:"required"`
	PollInterval time.Duration `yaml:"poll_interval"`
	Debounce     time.Duration `yaml:"debounce"`
	Paths        []Path        `yaml:"paths" validate:"dive"`
}

// Path defines a prefix and depth within a mirror.
type Path struct {
	Prefix string `yaml:"prefix"`
	Depth  int    `yaml:"depth" validate:"gt=0"`
}

// Load reads and parses a config file from disk.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return Parse(data)
}

// Parse parses config from YAML bytes.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Check empty prefixes before applying defaults
	// (default paths use empty prefix which is valid)
	if err := checkEmptyPrefixes(&cfg); err != nil {
		return nil, err
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(c *Config) {
	for i := range c.Mirrors {
		m := &c.Mirrors[i]
		if m.PollInterval == 0 {
			m.PollInterval = 30 * time.Second
		}
		if m.Debounce == 0 {
			m.Debounce = 2 * time.Minute
		}
		if m.Paths == nil {
			m.Paths = []Path{{Prefix: "", Depth: 1}}
		}
	}
}

func validate(c *Config) error {
	v := validator.New()
	if err := v.Struct(c); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}
	if err := checkDuplicateNames(c); err != nil {
		return err
	}
	if err := checkOverlaps(c); err != nil {
		return err
	}
	return nil
}

func checkEmptyPrefixes(c *Config) error {
	for _, m := range c.Mirrors {
		if m.Paths == nil {
			continue
		}
		for _, p := range m.Paths {
			if p.Prefix == "" {
				return fmt.Errorf("mirror %q: path prefix cannot be empty when explicitly listed", m.Name)
			}
		}
	}
	return nil
}

func checkDuplicateNames(c *Config) error {
	seen := make(map[string]bool)
	for _, m := range c.Mirrors {
		if seen[m.Name] {
			return fmt.Errorf("duplicate mirror name: %q", m.Name)
		}
		seen[m.Name] = true
	}
	return nil
}

func checkOverlaps(c *Config) error {
	for i := 0; i < len(c.Mirrors); i++ {
		for j := i + 1; j < len(c.Mirrors); j++ {
			mi, mj := c.Mirrors[i], c.Mirrors[j]
			if pathsOverlap(mi.Source, mj.Source) {
				return fmt.Errorf("mirrors %q and %q have overlapping sources: %s, %s",
					mi.Name, mj.Name, mi.Source, mj.Source)
			}
			if pathsOverlap(mi.Target, mj.Target) {
				return fmt.Errorf("mirrors %q and %q have overlapping targets: %s, %s",
					mi.Name, mj.Name, mi.Target, mj.Target)
			}
		}
	}
	return nil
}

func pathsOverlap(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	aSep := a + string(filepath.Separator)
	bSep := b + string(filepath.Separator)
	return strings.HasPrefix(bSep, aSep) || strings.HasPrefix(aSep, bSep)
}
