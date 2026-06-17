package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
)

func TestLoadDiagnosticConfig_AddsCustomRules(t *testing.T) {
	originalLen := len(classificationRules)

	dir := t.TempDir()
	path := filepath.Join(dir, "diagnostics.yaml")
	_ = os.WriteFile(path, []byte(`
patterns:
  ptp_clock_drift:
    conditions:
      - "clock drift exceeded"
      - "holdover timeout"
    classification: test_failure
    can_retry: false
    recommendation: "Check NIC firmware"
  node_not_ready:
    conditions:
      - "nodenotready"
    classification: infra_failure
    can_retry: true
`), 0o644)

	LoadDiagnosticConfig(path)

	if len(classificationRules) <= originalLen {
		t.Errorf("rules not added: before=%d after=%d", originalLen, len(classificationRules))
	}

	class, _ := classifyLog("the clock drift exceeded threshold during test")
	if class != domain.FailureTest {
		t.Errorf("class = %s; want test_failure", class)
	}

	class2, retry := classifyLog("node status: nodenotready")
	if class2 != domain.FailureInfra {
		t.Errorf("class = %s; want infra_failure", class2)
	}
	if !retry {
		t.Error("expected can_retry=true for node_not_ready")
	}

	classificationRules = classificationRules[len(classificationRules)-originalLen:]
}

func TestLoadDiagnosticConfig_MissingFile(t *testing.T) {
	originalLen := len(classificationRules)
	LoadDiagnosticConfig("/nonexistent/path.yaml")
	if len(classificationRules) != originalLen {
		t.Error("rules should not change for missing file")
	}
}
