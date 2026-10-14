package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shadi/triage-logprobs/internal/decision"
	"github.com/shadi/triage-logprobs/internal/llm/ollama"
)

type optionFlags []decision.Option

func (o *optionFlags) String() string {
	keys := make([]string, len(*o))
	for i, opt := range *o {
		keys[i] = opt.Key
	}
	return strings.Join(keys, ",")
}

func (o *optionFlags) Set(v string) error {
	key, desc, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return fmt.Errorf("want key=description, got %q", v)
	}
	*o = append(*o, decision.Option{Key: strings.TrimSpace(key), Description: strings.TrimSpace(desc)})
	return nil
}

var defaultOptions = []decision.Option{
	{Key: "sev1", Description: "SEV1 - critical: customer-facing outage or severe degradation across the service"},
	{Key: "sev2", Description: "SEV2 - major: significant degradation or partial outage, customers noticeably affected"},
	{Key: "sev3", Description: "SEV3 - minor: limited or internal impact, no urgent customer effect"},
}

type output struct {
	decision.Answer
	Model     string `json:"model"`
	LatencyMS int64  `json:"latency_ms"`
}

func main() {
	var opts optionFlags
	state := flag.String("state", "", "the state to decide on (required)")
	model := flag.String("model", decision.DefaultModel, "Ollama model")
	host := flag.String("host", ollama.DefaultBaseURL, "Ollama base URL")
	system := flag.String("system", decision.DefaultSystem, "system prompt (empty to omit)")
	minMass := flag.Float64("min-mass", decision.DefaultMinOptionMass, "minimum probability mass on the options")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall timeout")
	printPrompt := flag.Bool("print-prompt", false, "print the raw prompt to stderr")
	debias := flag.Bool("debias", false, "ask in forward and reversed option order and combine")
	flag.Var(&opts, "opt", "option as key=description; repeat for each option (default: sev1/sev2/sev3)")
	flag.Parse()

	if strings.TrimSpace(*state) == "" {
		usageError("-state is required")
	}
	if flag.NArg() > 0 {
		usageError(fmt.Sprintf("unexpected arguments %q (quote the -state value)", flag.Args()))
	}
	if !(*minMass > 0 && *minMass <= 1) {
		usageError("-min-mass must be in (0, 1]")
	}
	if *timeout <= 0 {
		usageError("-timeout must be positive")
	}
	if len(opts) == 0 {
		opts = defaultOptions
	}

	e, err := decision.New(ollama.New(*host),
		decision.WithModel(*model),
		decision.WithSystem(*system),
		decision.WithMinOptionMass(*minMass),
		decision.WithDebias(*debias),
	)
	if err != nil {
		usageError(err.Error())
	}

	if *printPrompt {
		p, err := decision.BuildPrompt(decision.QwenNoThink, decision.DefaultFormat, *system, *state, decision.Question{Kind: decision.KindChoice, Instructions: decision.DefaultChoiceInstructions, Options: opts})
		if err != nil {
			usageError(err.Error())
		}
		fmt.Fprintf(os.Stderr, "%s\n---\n", p)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	start := time.Now()
	res, err := e.Choose(ctx, *state, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output{Answer: res, Model: *model, LatencyMS: time.Since(start).Milliseconds()}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usageError(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	flag.Usage()
	os.Exit(2)
}
