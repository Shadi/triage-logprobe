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
	timeout                         time.Duration
	debias                          bool
}

func addCommonFlags(fs *flag.FlagSet, c *common, timeout time.Duration, timeoutUsage string) {
	fs.StringVar(&c.prom, "prom", "", "Prometheus base URL, e.g. http://prometheus:9090 (required)")
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
	if c.timeout <= 0 {
		return fmt.Errorf("-timeout must be positive")
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
