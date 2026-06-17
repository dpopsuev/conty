package app

import (
	"log/slog"
	"os"

	"github.com/dpopsuev/conty/internal/domain"
	"gopkg.in/yaml.v3"
)

// DiagnosticConfig is the YAML format for custom failure classification rules.
type DiagnosticConfig struct {
	Patterns map[string]struct {
		Conditions     []string `yaml:"conditions"`
		Classification string   `yaml:"classification"`
		CanRetry       bool     `yaml:"can_retry"`
		Recommendation string   `yaml:"recommendation"`
	} `yaml:"patterns"`
}

// LoadDiagnosticConfig reads a YAML file and prepends rules to the classifier.
// Custom rules take priority over built-in defaults.
func LoadDiagnosticConfig(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("diagnostic config not loaded", "path", path, "error", err)
		return
	}

	var cfg DiagnosticConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		slog.Warn("diagnostic config parse error", "path", path, "error", err)
		return
	}

	var custom []classificationRule
	for name, pat := range cfg.Patterns {
		class := domain.FailureClassification(pat.Classification)
		if class == "" {
			class = domain.FailureUnknown
		}
		custom = append(custom, classificationRule{
			class:    class,
			canRetry: pat.CanRetry,
			patterns: pat.Conditions,
		})
		slog.Info("diagnostic rule loaded", "name", name, "patterns", len(pat.Conditions), "class", class)
	}

	if len(custom) > 0 {
		classificationRules = append(custom, classificationRules...)
		slog.Info("diagnostic config applied", "path", path, "custom_rules", len(custom))
	}
}
