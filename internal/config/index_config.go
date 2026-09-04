package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var defaultPatterns = []string{"PLAN.md", "PROGRESS.md"}

// IndexConfig controls fd-based plan file discovery during full index.
type IndexConfig struct {
	Directories []string `json:"directories"`
	Patterns    []string `json:"patterns"`
}

// IndexConfigPath returns the default index config JSON path (XDG config).
func IndexConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			base = "."
		} else {
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "chv", "index.json")
}

// LoadIndexConfig reads config from path. Missing file returns defaults.
func LoadIndexConfig(path string) (IndexConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return IndexConfig{}, nil
		}
		return IndexConfig{}, err
	}
	var cfg IndexConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return IndexConfig{}, err
	}
	return cfg, nil
}

// PatternsOrDefault returns configured patterns or PLAN.md + PROGRESS.md.
func (c IndexConfig) PatternsOrDefault() []string {
	if len(c.Patterns) == 0 {
		return append([]string(nil), defaultPatterns...)
	}
	return c.Patterns
}

// ExpandDirectories expands ~ paths and returns cleaned absolute directories.
func (c IndexConfig) ExpandDirectories(home string) []string {
	out := make([]string, 0, len(c.Directories))
	for _, dir := range c.Directories {
		dir = expandHome(dir, home)
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		out = append(out, filepath.Clean(abs))
	}
	return out
}

func expandHome(path, home string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
