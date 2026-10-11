package main

import (
	"errors"
	"flag"
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
		"unknown flag":    append(p, "-nope"),
		"reserved rubric": append(p, "-metric", "a=up", "-rubric", badRubric),
	} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}

	cfg, err = parseFlags(append(p, "-metric", "a=up", "-at", "2026-10-11T19:00:00.5+03:00"))
	if err != nil || !cfg.at.Equal(time.Date(2026, 10, 11, 16, 0, 0, 500_000_000, time.UTC)) {
		t.Errorf("offset -at: %v %v", cfg.at, err)
	}
	if _, err := parseFlags([]string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h: err = %v, want flag.ErrHelp", err)
	}
}
