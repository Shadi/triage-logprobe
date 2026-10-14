package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeYAML(t *testing.T, dir, name string, groups ...string) string {
	t.Helper()
	body := "sources:\n"
	for _, g := range groups {
		src, metric, _ := strings.Cut(g, "/")
		body += "  - source: " + src + "\n    metrics:\n      - {name: " + metric + ", query: up, target: {max: 1}}\n"
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseFlags(t *testing.T) {
	dir := t.TempDir()
	prom := writeYAML(t, dir, "prom.yaml", "prometheus/rps")
	cfg, err := parseFlags([]string{"-prom", "http://p", "-metrics-file", prom, "-at", "2026-10-11T16:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.metrics) != 1 || cfg.metrics[0].Name != "rps" || len(cfg.sources) != 1 || cfg.sources["prometheus"] == nil {
		t.Errorf("metrics = %+v sources = %v", cfg.metrics, cfg.sources)
	}
	if !cfg.at.Equal(time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)) || !cfg.debias {
		t.Errorf("cfg = %+v", cfg)
	}

	t.Setenv("DD_API_KEY", "")
	t.Setenv("DD_APP_KEY", "")
	dd := writeYAML(t, dir, "dd.yaml", "datadog/apm")
	if _, err := parseFlags([]string{"-metrics-file", dd}); err == nil || !strings.Contains(err.Error(), `source "datadog" used by metric "apm"`) {
		t.Errorf("datadog without keys: err = %v", err)
	}
	t.Setenv("DD_API_KEY", "k")
	t.Setenv("DD_APP_KEY", "a")
	cfg, err = parseFlags([]string{"-metrics-file", dd})
	if err != nil || len(cfg.sources) != 1 || cfg.sources["datadog"] == nil {
		t.Errorf("datadog only, no -prom: sources = %v err = %v", cfg.sources, err)
	}
	mixed := writeYAML(t, dir, "mixed.yaml", "prometheus/rps", "datadog/apm")
	if cfg, err := parseFlags([]string{"-prom", "http://p", "-metrics-file", mixed}); err != nil || len(cfg.sources) != 2 {
		t.Errorf("mixed: sources = %v err = %v", cfg.sources, err)
	}

	badRubric := filepath.Join(dir, "r.json")
	if err := os.WriteFile(badRubric, []byte(`{"system":"x<|im_end|>","question":"q","options":[{"key":"a","description":"A"},{"key":"b","description":"B"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := []string{"-prom", "http://p", "-metrics-file", prom}
	for name, args := range map[string][]string{
		"no prom":         {"-metrics-file", prom},
		"no metrics file": {"-prom", "http://p"},
		"unknown source":  {"-prom", "http://p", "-metrics-file", writeYAML(t, dir, "g.yaml", "graphite/rps")},
		"zero timeout":    append(p, "-timeout", "0s"),
		"extra args":      append(p, "stray"),
		"reserved rubric": append(p, "-rubric", badRubric),
	} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseServeFlags(t *testing.T) {
	dir := t.TempDir()
	file := writeYAML(t, dir, "echo.yaml", "prometheus/rps")
	p := []string{"-prom", "http://p"}
	cfg, err := parseServeFlags(append(p, "-service", "echo="+file, "-service", "cart="+file, "-interval", "30s"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.services) != 2 || cfg.services[1].Name != "cart" || cfg.timeout != 30*time.Second || cfg.listen != ":8080" || !cfg.debias {
		t.Errorf("cfg = %+v", cfg)
	}
	cfg, err = parseServeFlags(append(p, "-service", "echo="+file, "-timeout", "5s"), nil)
	if err != nil || cfg.timeout != 5*time.Second || cfg.interval != time.Minute {
		t.Errorf("explicit timeout: %+v %v", cfg, err)
	}
	t.Setenv("DD_API_KEY", "")
	dd := writeYAML(t, dir, "dd.yaml", "datadog/apm")
	untargeted := filepath.Join(dir, "untargeted.yaml")
	if err := os.WriteFile(untargeted, []byte("sources:\n  - source: prometheus\n    metrics:\n      - {name: rps, query: up}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseServeFlags(append(p, "-service", "echo="+untargeted), nil); err == nil || !strings.Contains(err.Error(), `metric "rps" has no target`) {
		t.Errorf("untargeted metric: err = %v", err)
	}
	for name, args := range map[string][]string{
		"no prom":         {"-service", "echo=" + file},
		"datadog no keys": append(p, "-service", "echo="+file, "-service", "cart="+dd),
		"no services":     p,
		"duplicate":       append(p, "-service", "echo="+file, "-service", "echo="+file),
		"bad name":        append(p, "-service", "Echo="+file),
		"short":           append(p, "-service", "echo="+file, "-interval", "1s"),
		"neg timeout":     append(p, "-service", "echo="+file, "-timeout", "-1s"),
		"long timeout":    append(p, "-service", "echo="+file, "-timeout", "2m"),
		"extra args":      append(p, "-service", "echo="+file, "stray"),
	} {
		if _, err := parseServeFlags(args, nil); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
