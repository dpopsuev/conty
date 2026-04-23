package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	appName    = "conty"
	configFile = "config.yaml"
)

type Config struct {
	Backends  map[string]Backend  `yaml:"backends"`
	Pipelines map[string]Pipeline `yaml:"pipelines"`
}

type Backend struct {
	Type     string `yaml:"type,omitempty"`
	URL      string `yaml:"url,omitempty"`
	User     string `yaml:"user,omitempty"`
	UserEnv  string `yaml:"user_env,omitempty"`
	Token    string `yaml:"token,omitempty"`
	TokenEnv string `yaml:"token_env,omitempty"`
}

func (b Backend) ResolveType(key string) string {
	if b.Type != "" {
		return b.Type
	}
	return key
}

func (b Backend) ResolveToken() string {
	if b.Token != "" {
		return b.Token
	}
	if b.TokenEnv != "" {
		return os.Getenv(b.TokenEnv)
	}
	return ""
}

func (b Backend) ResolveUser() string {
	if b.User != "" {
		return b.User
	}
	if b.UserEnv != "" {
		return os.Getenv(b.UserEnv)
	}
	return ""
}

type Pipeline struct {
	Backend string         `yaml:"backend"`
	Steps   []PipelineStep `yaml:"steps"`
}

type PipelineStep struct {
	Job    string            `yaml:"job"`
	Params map[string]string `yaml:"params,omitempty"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

func Exists(path string) bool {
	if path == "" {
		path = DefaultPath()
	}
	_, err := os.Stat(path)
	return err == nil
}

func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, appName, configFile)
}
