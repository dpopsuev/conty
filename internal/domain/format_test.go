package domain_test

import (
	"testing"

	"github.com/dpopsuev/conty/internal/domain"
)

func TestFmtDuration(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "0s"},
		{500, "0s"},       // sub-second rounds to 0s
		{1000, "1s"},
		{90000, "1m30s"},
		{3661000, "1h1m1s"},
		{7450490, "2h4m10s"}, // real value from Wait for DU policies stage
		{1079157, "17m59s"},  // Mirror stage
		{84, "0s"},           // very short skipped stage
	}
	for _, tc := range cases {
		got := domain.FmtDuration(tc.ms)
		if got != tc.want {
			t.Errorf("FmtDuration(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestCleanStepDesc(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Shell Script", "Shell Script"},
		{"  \n  oc wait --for=condition=Updated mcp master\n  ", "oc wait --for=condition=Updated mcp master"},
		{"\n                    source ocp-edge-venv/bin/activate\n                    ansible-playbook -v \\\n                        -i ~/clusterconfigs/ocp-edge.inventory",
			"source ocp-edge-venv/bin/activate"},
		{"echo 'done'", "echo 'done'"},
		{"", ""},
	}
	for _, tc := range cases {
		got := domain.CleanStepDesc(tc.in)
		if got != tc.want {
			t.Errorf("CleanStepDesc(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
