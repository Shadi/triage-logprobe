package triage

import (
	"context"
	"strings"
	"time"

	"github.com/shadi/triage-logprobe/internal/decision"
	"github.com/shadi/triage-logprobe/internal/metrics"
)

type Params struct {
	At       time.Time
	Window   time.Duration
	Baseline time.Duration
}

type Result struct {
	Severity  string           `json:"severity"`
	Answer    decision.Answer  `json:"answer"`
	State     string           `json:"state"`
	Readings  []Reading        `json:"readings,omitempty"`
	At        time.Time        `json:"at"`
	Window    string           `json:"window"`
	Baseline  string           `json:"baseline"`
	LatencyMS map[string]int64 `json:"latency_ms"`
}

func Check(ctx context.Context, src metrics.Source, e *decision.Engine, r Rubric, ms []Metric, p Params) (Result, error) {
	t0 := time.Now()
	readings, err := Collect(ctx, src, ms, p.At, p.Window, p.Baseline)
	if err != nil {
		return Result{}, err
	}
	t1 := time.Now()
	state := RenderState(readings, p.Window, p.Baseline)
	ans, err := Assess(ctx, e, r, state)
	if err != nil {
		return Result{}, err
	}
	t2 := time.Now()
	return Result{
		Severity:  strings.ToUpper(ans.Choice),
		Answer:    ans,
		State:     state,
		Readings:  readings,
		At:        p.At.UTC(),
		Window:    p.Window.String(),
		Baseline:  p.Baseline.String(),
		LatencyMS: map[string]int64{"metrics": t1.Sub(t0).Milliseconds(), "model": t2.Sub(t1).Milliseconds()},
	}, nil
}
