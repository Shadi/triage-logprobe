package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shadi/triage-logprobe/internal/decision"
	"github.com/shadi/triage-logprobe/internal/llm/ollama"
	"github.com/shadi/triage-logprobe/internal/metrics/prometheus"
	"github.com/shadi/triage-logprobe/internal/triage"
)

type metricFlags []triage.Metric

func (m *metricFlags) String() string {
	names := make([]string, len(*m))
	for i, x := range *m {
		names[i] = x.Name
	}
	return strings.Join(names, ",")
}

func (m *metricFlags) Set(v string) error {
	x, err := triage.ParseMetric(v)
	if err != nil {
		return err
	}
	*m = append(*m, x)
	return nil
}

type config struct {
	prom, ollama, model, rubricPath, metricsFile string
	rubric                                       triage.Rubric
	metrics                                      metricFlags
	at                                           time.Time
	window, baseline, timeout                    time.Duration
	debias, asJSON, showState                    bool
}

type output struct {
	Severity  string           `json:"severity"`
	Answer    decision.Answer  `json:"answer"`
	State     string           `json:"state"`
	Readings  []triage.Reading `json:"readings"`
	At        time.Time        `json:"at"`
	Window    string           `json:"window"`
	Baseline  string           `json:"baseline"`
	Model     string           `json:"model"`
	Debias    bool             `json:"debias"`
	LatencyMS map[string]int64 `json:"latency_ms"`
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFlags(args []string) (config, error) {
	var cfg config
	var at string
	fs := flag.NewFlagSet("triage-logprobe", flag.ContinueOnError)
	fs.StringVar(&cfg.prom, "prom", "", "Prometheus base URL, e.g. http://prometheus:9090 (required)")
	fs.Var(&cfg.metrics, "metric", "metric as name=PROMQL; repeatable")
	fs.StringVar(&cfg.metricsFile, "metrics-file", "", "file with one name=PROMQL per line (# comments allowed)")
	fs.StringVar(&at, "at", "", "evaluation time, RFC3339 (default now)")
	fs.DurationVar(&cfg.window, "window", 5*time.Minute, "how far back the incident may reach; the baseline ends before it")
	fs.DurationVar(&cfg.baseline, "baseline", 30*time.Minute, "length of the normal period before the window")
	fs.StringVar(&cfg.ollama, "ollama", ollama.DefaultBaseURL, "Ollama base URL")
	fs.StringVar(&cfg.model, "model", decision.DefaultModel, "Ollama model")
	fs.StringVar(&cfg.rubricPath, "rubric", "", "rubric JSON file (default: built-in)")
	fs.BoolVar(&cfg.debias, "debias", true, "ask in forward and reversed option order")
	fs.BoolVar(&cfg.asJSON, "json", false, "print JSON")
	fs.BoolVar(&cfg.showState, "show-state", false, "print the state sent to the model")
	fs.DurationVar(&cfg.timeout, "timeout", 2*time.Minute, "overall timeout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, err
		}
		return cfg, fmt.Errorf("run with -h for usage")
	}
	if cfg.prom == "" {
		return cfg, fmt.Errorf("-prom is required")
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	if cfg.metricsFile != "" {
		f, err := os.Open(cfg.metricsFile)
		if err != nil {
			return cfg, err
		}
		defer f.Close()
		ms, err := triage.ParseMetrics(f)
		if err != nil {
			return cfg, fmt.Errorf("%s: %w", cfg.metricsFile, err)
		}
		cfg.metrics = append(ms, cfg.metrics...)
	}
	if len(cfg.metrics) == 0 {
		return cfg, fmt.Errorf("at least one -metric or -metrics-file is required")
	}
	if err := triage.CheckUnique(cfg.metrics); err != nil {
		return cfg, err
	}
	if cfg.window <= 0 || cfg.baseline <= 0 || cfg.timeout <= 0 {
		return cfg, fmt.Errorf("-window, -baseline and -timeout must be positive")
	}
	cfg.at = time.Now()
	if at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return cfg, fmt.Errorf("-at %q: want RFC3339 with a zone, e.g. 2026-10-11T16:00:00Z or 2026-10-11T19:00:00+03:00", at)
		}
		cfg.at = t
	}
	r, err := triage.LoadRubric(cfg.rubricPath)
	if err != nil {
		return cfg, err
	}
	if _, err := decision.BuildPrompt(decision.QwenNoThink, decision.DefaultFormat, r.System, "check", r.SevQuestion()); err != nil {
		return cfg, fmt.Errorf("rubric: %w", err)
	}
	cfg.rubric = r
	return cfg, nil
}

func run(cfg config) error {
	rubric := cfg.rubric
	prom, err := prometheus.New(cfg.prom)
	if err != nil {
		return err
	}
	prom.Warn = func(q, w string) { fmt.Fprintf(os.Stderr, "prometheus warning: %s (%s)\n", w, q) }
	engine, err := decision.New(ollama.New(cfg.ollama),
		decision.WithModel(cfg.model),
		decision.WithSystem(rubric.System),
		decision.WithDebias(cfg.debias),
	)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	t0 := time.Now()
	readings, err := triage.Collect(ctx, prom, cfg.metrics, cfg.at, cfg.window, cfg.baseline)
	if err != nil {
		return err
	}
	t1 := time.Now()
	state := triage.RenderState(readings, cfg.window, cfg.baseline)
	ans, err := triage.Assess(ctx, engine, rubric, state)
	if err != nil {
		return err
	}
	t2 := time.Now()

	out := output{
		Severity:  strings.ToUpper(ans.Choice),
		Answer:    ans,
		State:     state,
		Readings:  readings,
		At:        cfg.at.UTC(),
		Window:    cfg.window.String(),
		Baseline:  cfg.baseline.String(),
		Model:     cfg.model,
		Debias:    cfg.debias,
		LatencyMS: map[string]int64{"metrics": t1.Sub(t0).Milliseconds(), "model": t2.Sub(t1).Milliseconds()},
	}
	if cfg.asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	printText(out, rubric, cfg.showState)
	return nil
}

func printText(o output, r triage.Rubric, showState bool) {
	if showState {
		fmt.Printf("%s\n\n", o.State)
	}
	parts := make([]string, len(r.Options))
	for i, opt := range r.Options {
		parts[i] = fmt.Sprintf("%s %.2f", opt.Key, o.Answer.Probs[opt.Key])
	}
	fmt.Printf("%s  %s   confidence %.2f  margin %.2f  option_mass %.2f\n",
		o.Severity, strings.Join(parts, " | "), o.Answer.Confidence, o.Answer.Margin, o.Answer.OptionMass)
	fmt.Printf("at %s  metrics %dms  model %dms  (%s, debias=%v)\n",
		o.At.Format(time.RFC3339), o.LatencyMS["metrics"], o.LatencyMS["model"], o.Model, o.Debias)
}
