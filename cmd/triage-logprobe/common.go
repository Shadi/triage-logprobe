package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/shadi/triage-logprobe/internal/decision"
	"github.com/shadi/triage-logprobe/internal/llm/ollama"
	"github.com/shadi/triage-logprobe/internal/metrics/prometheus"
	"github.com/shadi/triage-logprobe/internal/triage"
)

type common struct {
	prom, ollama, model, rubricPath string
	rubric                          triage.Rubric
	window, baseline, timeout       time.Duration
	debias                          bool
}

func addCommonFlags(fs *flag.FlagSet, c *common, timeout time.Duration, timeoutUsage string) {
	fs.StringVar(&c.prom, "prom", "", "Prometheus base URL, e.g. http://prometheus:9090 (required)")
	fs.DurationVar(&c.window, "window", 5*time.Minute, "how far back the incident may reach; the baseline ends before it")
	fs.DurationVar(&c.baseline, "baseline", 30*time.Minute, "length of the normal period before the window")
	fs.StringVar(&c.ollama, "ollama", ollama.DefaultBaseURL, "Ollama base URL")
	fs.StringVar(&c.model, "model", decision.DefaultModel, "Ollama model")
	fs.StringVar(&c.rubricPath, "rubric", "", "rubric JSON file (default: built-in)")
	fs.BoolVar(&c.debias, "debias", true, "ask in forward and reversed option order")
	fs.DurationVar(&c.timeout, "timeout", timeout, timeoutUsage)
}

func (c *common) validate() error {
	if c.prom == "" {
		return fmt.Errorf("-prom is required")
	}
	if c.window <= 0 || c.baseline <= 0 || c.timeout <= 0 {
		return fmt.Errorf("-window, -baseline and -timeout must be positive")
	}
	r, err := triage.LoadRubric(c.rubricPath)
	if err != nil {
		return err
	}
	if _, err := decision.BuildPrompt(decision.QwenNoThink, decision.DefaultFormat, r.System, "check", r.SevQuestion()); err != nil {
		return fmt.Errorf("rubric: %w", err)
	}
	c.rubric = r
	return nil
}

func (c *common) clients(warn func(query, warning string)) (*prometheus.Client, *decision.Engine, error) {
	prom, err := prometheus.New(c.prom)
	if err != nil {
		return nil, nil, err
	}
	prom.Warn = warn
	engine, err := decision.New(ollama.New(c.ollama),
		decision.WithModel(c.model),
		decision.WithSystem(c.rubric.System),
		decision.WithDebias(c.debias),
	)
	if err != nil {
		return nil, nil, err
	}
	return prom, engine, nil
}
