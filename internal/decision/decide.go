package decision

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/shadi/triage-logprobe/internal/llm"
	"golang.org/x/sync/errgroup"
)

const (
	DefaultModel         = "qwen3.5:4b"
	DefaultSystem        = "You are a decision model. Select the option that best fits the state."
	DefaultTopN          = 20
	DefaultMinOptionMass = 0.5
)

var (
	ErrLowOptionMass = errors.New("decision: too little probability on the options")
	ErrNonFinite     = errors.New("decision: non-finite probabilities")
)

type Engine struct {
	source        llm.LogprobSource
	model         string
	template      Template
	format        Format
	system        string
	topN          int
	minOptionMass float64
	debias        bool
}

func (e *Engine) Choose(ctx context.Context, state string, opts []Option) (Answer, error) {
	answers, err := e.Decide(ctx, state, map[string]Question{
		"choice": {Kind: KindChoice, Instructions: DefaultChoiceInstructions, Options: opts},
	})
	if err != nil {
		return Answer{}, err
	}
	return answers["choice"], nil
}

func (e *Engine) Decide(ctx context.Context, state string, questions map[string]Question) (map[string]Answer, error) {
	if len(questions) == 0 {
		return nil, errors.New("decision: no questions")
	}
	ids := slices.Sorted(maps.Keys(questions))
	for _, id := range ids {
		q := questions[id]
		if err := q.Validate(); err != nil {
			return nil, fmt.Errorf("question %q: %w", id, err)
		}
		if _, err := buildPrompt(e.template, e.format, e.system, state, q, q.options()); err != nil {
			return nil, fmt.Errorf("question %q: %w", id, err)
		}
	}

	runs := make([][]raw, len(ids))
	g, gctx := errgroup.WithContext(ctx)
	for qi, id := range ids {
		q := questions[id]
		ords := orders(len(q.options()), e.debias)
		runs[qi] = make([]raw, len(ords))
		for oi, o := range ords {
			g.Go(func() error {
				r, err := e.scoreOnce(gctx, state, q, o)
				if err != nil {
					return fmt.Errorf("question %q: %w", id, err)
				}
				runs[qi][oi] = r
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	answers := make(map[string]Answer, len(ids))
	for qi, id := range ids {
		a, err := finalize(questions[id], runs[qi], e.minOptionMass)
		if err != nil {
			return nil, fmt.Errorf("question %q: %w", id, err)
		}
		answers[id] = a
	}
	return answers, nil
}

func (e *Engine) scoreOnce(ctx context.Context, state string, q Question, order []int) (raw, error) {
	opts := q.options()
	slots := make([]Option, len(order))
	for slot, idx := range order {
		slots[slot] = opts[idx]
	}
	prompt, err := buildPrompt(e.template, e.format, e.system, state, q, slots)
	if err != nil {
		return raw{}, err
	}

	toks, err := e.source.NextTokenLogprobs(ctx, e.model, prompt, e.topN)
	if err != nil {
		return raw{}, fmt.Errorf("decision: %w", err)
	}

	lp, err := OptionLogprobs(toks, len(slots))
	if err != nil {
		return raw{}, err
	}
	floor, hasFloor := floorOf(toks, len(slots))
	return raw{order: order, logprobs: lp, floor: floor, hasFloor: hasFloor, mass: OptionMass(lp)}, nil
}

func floorOf(toks []llm.TokenLogprob, k int) (float64, bool) {
	floor := math.Inf(1)
	hasNonOption := false
	for _, t := range toks {
		if math.IsNaN(t.Logprob) || math.IsInf(t.Logprob, 0) {
			continue
		}
		floor = math.Min(floor, math.Min(t.Logprob, 0))
		if _, ok := optionIndex(t.Text(), k); !ok {
			hasNonOption = true
		}
	}
	return floor, hasNonOption
}

func identityOrder(k int) []int {
	o := make([]int, k)
	for i := range o {
		o[i] = i
	}
	return o
}

func orders(k int, debias bool) [][]int {
	fwd := identityOrder(k)
	if !debias {
		return [][]int{fwd}
	}
	rev := make([]int, k)
	for i := range rev {
		rev[i] = k - 1 - i
	}
	return [][]int{fwd, rev}
}
