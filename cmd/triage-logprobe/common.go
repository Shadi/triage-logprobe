package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/shadi/triage-logprobe/internal/decision"
	"github.com/shadi/triage-logprobe/internal/llm/ollama"
	"github.com/shadi/triage-logprobe/internal/metrics"
	"github.com/shadi/triage-logprobe/internal/metrics/datadog"
	"github.com/shadi/triage-logprobe/internal/metrics/prometheus"
	"github.com/shadi/triage-logprobe/internal/triage"
)

type warnFunc func(query, warning string)

type common struct {
	prom, ollama, model, rubricPath string
	rubric                          triage.Rubric
	sources                         metrics.Sources
	timeout                         time.Duration
	debias                          bool
}

type sourceFactory func(c *common, warn warnFunc) (metrics.Source, error)

var sourceFactories = map[string]sourceFactory{
	"prometheus": newPrometheus,
	"datadog":    newDatadog,
}

func newPrometheus(c *common, warn warnFunc) (metrics.Source, error) {
	if c.prom == "" {
		return nil, fmt.Errorf("-prom is required")
	}
	p, err := prometheus.New(c.prom)
	if err != nil {
		return nil, err
	}
	p.Warn = warn
	return p, nil
}

func newDatadog(*common, warnFunc) (metrics.Source, error) {
	return datadog.New(os.Getenv("DD_API_KEY"), os.Getenv("DD_APP_KEY"), os.Getenv("DD_SITE"))
}

func addCommonFlags(fs *flag.FlagSet, c *common, timeout time.Duration, timeoutUsage string) {
	fs.StringVar(&c.prom, "prom", "", "Prometheus base URL, e.g. http://prometheus:9090 (required when a metric uses source prometheus)")
	fs.StringVar(&c.ollama, "ollama", ollama.DefaultBaseURL, "Ollama base URL")
	fs.StringVar(&c.model, "model", decision.DefaultModel, "Ollama model")
	fs.StringVar(&c.rubricPath, "rubric", "", "rubric JSON file (default: rubric.json built into the binary)")
	fs.BoolVar(&c.debias, "debias", true, "ask in forward and reversed option order")
	fs.DurationVar(&c.timeout, "timeout", timeout, timeoutUsage)
}

func (c *common) validate(ms []triage.Metric, warn warnFunc) error {
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
	c.sources, err = c.buildSources(ms, warn)
	return err
}

func (c *common) buildSources(ms []triage.Metric, warn warnFunc) (metrics.Sources, error) {
	out := metrics.Sources{}
	for _, m := range ms {
		if _, ok := out[m.Source]; ok {
			continue
		}
		f, ok := sourceFactories[m.Source]
		if !ok {
			return nil, fmt.Errorf("metric %q: unknown source %q, want one of %s", m.Name, m.Source, strings.Join(slices.Sorted(maps.Keys(sourceFactories)), ", "))
		}
		src, err := f(c, warn)
		if err != nil {
			return nil, fmt.Errorf("source %q used by metric %q: %w", m.Source, m.Name, err)
		}
		out[m.Source] = src
	}
	return out, nil
}

func (c *common) engine() (*decision.Engine, error) {
	return decision.New(ollama.New(c.ollama),
		decision.WithModel(c.model),
		decision.WithSystem(c.rubric.System),
		decision.WithDebias(c.debias),
	)
}
