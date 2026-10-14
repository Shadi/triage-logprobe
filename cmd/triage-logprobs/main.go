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

	"github.com/shadi/triage-logprobs/internal/triage"
)

type config struct {
	common
	metricsFile       string
	metrics           []triage.Metric
	at                time.Time
	asJSON, showState bool
}

type output struct {
	triage.Result
	Model  string `json:"model"`
	Debias bool   `json:"debias"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		os.Exit(serveMain(os.Args[2:]))
	}
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
	fs := flag.NewFlagSet("triage-logprobs", flag.ContinueOnError)
	addCommonFlags(fs, &cfg.common, 2*time.Minute, "overall timeout")
	fs.StringVar(&cfg.metricsFile, "metrics-file", "", "YAML file with the metrics to evaluate (required)")
	fs.StringVar(&at, "at", "", "evaluation time, RFC3339 (default now)")
	fs.BoolVar(&cfg.asJSON, "json", false, "print JSON")
	fs.BoolVar(&cfg.showState, "show-state", false, "print the state sent to the model")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: triage-logprobs [flags]\n       triage-logprobs serve [flags]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, err
		}
		return cfg, fmt.Errorf("run with -h for usage")
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	if cfg.metricsFile == "" {
		return cfg, fmt.Errorf("-metrics-file is required")
	}
	ms, err := triage.LoadMetrics(cfg.metricsFile)
	if err != nil {
		return cfg, err
	}
	cfg.metrics = ms
	cfg.at = time.Now()
	if at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return cfg, fmt.Errorf("-at %q: want RFC3339 with a zone, e.g. 2026-10-11T16:00:00Z or 2026-10-11T19:00:00+03:00", at)
		}
		cfg.at = t
	}
	if err := cfg.validate(cfg.metrics, stderrWarn); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func stderrWarn(q, w string) {
	fmt.Fprintf(os.Stderr, "prometheus warning: %s (%s)\n", w, q)
}

func run(cfg config) error {
	engine, err := cfg.engine()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	res, err := triage.Check(ctx, cfg.sources, engine, cfg.rubric, cfg.metrics, triage.Params{At: cfg.at})
	if err != nil {
		return err
	}
	out := output{Result: res, Model: cfg.model, Debias: cfg.debias}
	if cfg.asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	printText(out, cfg.rubric, cfg.showState)
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
