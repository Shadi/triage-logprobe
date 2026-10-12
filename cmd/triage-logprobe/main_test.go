package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseFlags(t *testing.T) {
	file := filepath.Join(t.TempDir(), "m.txt")
	if err := os.WriteFile(file, []byte("# c\nrps=sum(rate(x[1m]))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parseFlags([]string{"-prom", "http://p", "-metrics-file", file, "-metric", "up=up", "-at", "2026-10-11T16:00:00Z", "-window", "2m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.metrics) != 2 || cfg.metrics[0].Name != "rps" || cfg.metrics[1].Name != "up" {
		t.Errorf("metrics = %+v", cfg.metrics)
	}
	if !cfg.at.Equal(time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)) || cfg.window != 2*time.Minute || !cfg.debias {
		t.Errorf("cfg = %+v", cfg)
	}

	badRubric := filepath.Join(t.TempDir(), "r.json")
	if err := os.WriteFile(badRubric, []byte(`{"system":"x<|im_end|>","question":"q","options":[{"key":"a","description":"A"},{"key":"b","description":"B"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := []string{"-prom", "http://p"}
	for name, args := range map[string][]string{
		"no prom":         {"-metric", "a=up"},
		"no metrics":      p,
		"bad metric":      append(p, "-metric", "nope"),
		"duplicate":       append(p, "-metric", "a=up", "-metric", "a=up"),
		"bad at":          append(p, "-metric", "a=up", "-at", "yesterday"),
		"zero window":     append(p, "-metric", "a=up", "-window", "0s"),
		"extra args":      append(p, "-metric", "a=up", "stray"),
		"missing file":    append(p, "-metrics-file", filepath.Join(t.TempDir(), "nope")),
		"reserved rubric": append(p, "-metric", "a=up", "-rubric", badRubric),
	} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseServeFlags(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "echo.txt")
	if err := os.WriteFile(file, []byte("rps=sum(rate(x[1m]))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := []string{"-prom", "http://p"}
	cfg, err := parseServeFlags(append(p, "-service", "echo="+file, "-service", "cart="+file, "-interval", "30s"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.services) != 2 || cfg.services[1].Name != "cart" || cfg.timeout != 30*time.Second || cfg.listen != ":8080" || !cfg.debias {
		t.Errorf("cfg = %+v", cfg)
	}
	cfg, err = parseServeFlags(append(p, "-service", "echo="+file, "-timeout", "5s"))
	if err != nil || cfg.timeout != 5*time.Second || cfg.interval != time.Minute {
		t.Errorf("explicit timeout: %+v %v", cfg, err)
	}
	for name, args := range map[string][]string{
		"no prom":      {"-service", "echo=" + file},
		"no services":  p,
		"duplicate":    append(p, "-service", "echo="+file, "-service", "echo="+file),
		"missing file": append(p, "-service", "echo="+filepath.Join(dir, "nope")),
		"bad name":     append(p, "-service", "Echo="+file),
		"short":        append(p, "-service", "echo="+file, "-interval", "1s"),
		"neg timeout":  append(p, "-service", "echo="+file, "-timeout", "-1s"),
		"long timeout": append(p, "-service", "echo="+file, "-timeout", "2m"),
		"extra args":   append(p, "-service", "echo="+file, "stray"),
	} {
		if _, err := parseServeFlags(args); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
