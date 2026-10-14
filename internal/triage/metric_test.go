package triage

import (
	"strings"
	"testing"
)

const twoSources = `
sources:
  - source: prometheus
    metrics:
      - name: error_ratio
        query: sum(rate(x{code=~"5.."}[2m])) / sum(rate(x[2m]))
        target: {max: 0.01}
      - name: rps
        query: "  sum(rate(x[2m]))  "
  - source: datadog
    metrics:
      - name: replicas
        query: avg:kubernetes_state.deployment.replicas_available{deployment:echo}
        target: {min: 2, max: 5}
`

func TestParseMetrics(t *testing.T) {
	ms, err := ParseMetrics(strings.NewReader(twoSources))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 {
		t.Fatalf("metrics = %+v", ms)
	}
	if ms[0].Source != "prometheus" || ms[0].Target.Min != nil || *ms[0].Target.Max != 0.01 || ms[0].Query != `sum(rate(x{code=~"5.."}[2m])) / sum(rate(x[2m]))` {
		t.Errorf("error_ratio = %+v", ms[0])
	}
	if ms[1].Name != "rps" || ms[1].Target != nil || ms[1].Query != "sum(rate(x[2m]))" {
		t.Errorf("rps = %+v", ms[1])
	}
	if ms[2].Source != "datadog" || *ms[2].Target.Min != 2 || *ms[2].Target.Max != 5 {
		t.Errorf("replicas = %+v", ms[2])
	}
}

func TestParseMetrics_Rejects(t *testing.T) {
	group := func(metrics string) string {
		return "sources:\n  - source: prometheus\n    metrics:\n" + metrics
	}
	for name, doc := range map[string]string{
		"empty file":      "",
		"no sources":      "sources: []\n",
		"bad source name": "sources:\n  - source: Prom\n    metrics:\n      - name: a\n        query: up\n",
		"empty group":     "sources:\n  - source: prometheus\n    metrics: []\n",
		"bad metric name": group("      - name: Bad\n        query: up\n"),
		"blank query":     group("      - name: a\n        query: '  '\n"),
		"empty target":    group("      - name: a\n        query: up\n        target: {}\n"),
		"nan target":      group("      - name: a\n        query: up\n        target: {max: .nan}\n"),
		"inf target":      group("      - name: a\n        query: up\n        target: {min: -.inf}\n"),
		"min above max":   group("      - name: a\n        query: up\n        target: {min: 5, max: 1}\n"),
		"duplicate":       twoSources + "      - name: rps\n        query: up\n",
		"two documents":   group("      - name: a\n        query: up\n") + "---\nsources: []\n",
	} {
		if _, err := ParseMetrics(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseMetrics_ErrorNamesPosition(t *testing.T) {
	_, err := ParseMetrics(strings.NewReader(twoSources + "      - name: x\n        query: up\n        target: {}\n"))
	if err == nil || !strings.Contains(err.Error(), "sources[1].metrics[1]") {
		t.Errorf("err = %v, want position", err)
	}
}

func TestCheckUnique(t *testing.T) {
	if err := CheckUnique([]Metric{{Name: "a"}, {Name: "b"}}); err != nil {
		t.Error(err)
	}
	if err := CheckUnique([]Metric{{Name: "a"}, {Name: "a"}}); err == nil {
		t.Error("expected duplicate error")
	}
}

func TestRequireTargets(t *testing.T) {
	ms, err := ParseMetrics(strings.NewReader(twoSources))
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireTargets(ms); err == nil || !strings.Contains(err.Error(), `"rps"`) {
		t.Errorf("err = %v, want rps named", err)
	}
	if err := RequireTargets([]Metric{ms[0], ms[2]}); err != nil {
		t.Error(err)
	}
}
