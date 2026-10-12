package decision

import (
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shadi/triage-logprobe/internal/llm"
)

type fakeSource struct {
	mu      sync.Mutex
	toks    []llm.TokenLogprob
	err     error
	prompt  string
	model   string
	topN    int
	prompts []string
}

func (f *fakeSource) NextTokenLogprobs(_ context.Context, model, prompt string, topN int) ([]llm.TokenLogprob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.model, f.prompt, f.topN = model, prompt, topN
	f.prompts = append(f.prompts, prompt)
	return f.toks, f.err
}

func TestChoose(t *testing.T) {
	src := &fakeSource{toks: []llm.TokenLogprob{tok("1", -0.427), tok("2", -1.146), tok("3", -3.73)}}
	e := mustNew(t, src)

	res, err := e.Choose(context.Background(), sevState, sevOptions)
	if err != nil {
		t.Fatal(err)
	}
	if res.Choice != "sev1" {
		t.Errorf("choice = %q, want sev1", res.Choice)
	}
	var sum float64
	for _, p := range res.Probs {
		sum += p
	}
	if math.Abs(sum-1) > eps || len(res.Probs) != 3 {
		t.Errorf("probs = %v, want 3 entries summing to 1", res.Probs)
	}
	if res.Probs["sev1"] <= res.Probs["sev2"] || res.Probs["sev2"] <= res.Probs["sev3"] {
		t.Errorf("probs not ordered as logprobs: %v", res.Probs)
	}
	if src.model != DefaultModel || src.topN != DefaultTopN || !strings.HasSuffix(src.prompt, DefaultFormat.Prefill) {
		t.Errorf("source got model=%q topN=%d prompt=%q", src.model, src.topN, src.prompt)
	}
}

func TestChoose_MapsPositionToKey(t *testing.T) {
	src := &fakeSource{toks: []llm.TokenLogprob{tok("3", -0.05), tok("1", -3), tok("SE", -9)}}
	res, err := mustNew(t, src).Choose(context.Background(), sevState, sevOptions)
	if err != nil {
		t.Fatal(err)
	}
	if res.Choice != "sev3" || res.Probs["sev2"] > 0.05 || res.Probs["sev2"] <= 0 {
		t.Errorf("res = %+v, want sev3 chosen and sev2 small but non-zero (floor)", res)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "sev2" {
		t.Errorf("missing = %v, want [sev2]", res.Missing)
	}
	if want := res.Probs["sev3"] - res.Probs["sev1"]; math.Abs(res.Margin-want) > eps {
		t.Errorf("margin = %v, want %v", res.Margin, want)
	}
}

func TestChoose_Tie(t *testing.T) {
	src := &fakeSource{toks: []llm.TokenLogprob{tok("1", math.Log(0.45)), tok("2", math.Log(0.45)), tok("3", math.Log(0.1))}}
	res, err := mustNew(t, src).Choose(context.Background(), sevState, sevOptions)
	if err != nil {
		t.Fatal(err)
	}
	if res.Margin > eps {
		t.Errorf("margin = %v, want 0 for a tie", res.Margin)
	}
}

func TestChoose_LowOptionMass(t *testing.T) {
	src := &fakeSource{toks: []llm.TokenLogprob{tok("[", -0.3), tok("1", -2), tok("2", -2.5)}}
	_, err := mustNew(t, src).Choose(context.Background(), sevState, sevOptions)
	if !errors.Is(err, ErrLowOptionMass) {
		t.Errorf("err = %v, want ErrLowOptionMass", err)
	}
}

func TestChoose_SourceError(t *testing.T) {
	boom := errors.New("boom")
	_, err := mustNew(t, &fakeSource{err: boom}).Choose(context.Background(), sevState, sevOptions)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrapped source error", err)
	}
}

func TestChoose_InvalidOptionsSkipsSource(t *testing.T) {
	src := &fakeSource{}
	if _, err := mustNew(t, src).Choose(context.Background(), sevState, sevOptions[:1]); err == nil {
		t.Error("expected error")
	}
	if src.prompt != "" {
		t.Error("source should not be called with invalid options")
	}
}

func TestOrders(t *testing.T) {
	if got := orders(3, false); len(got) != 1 || !slices.Equal(got[0], []int{0, 1, 2}) {
		t.Errorf("orders(3, false) = %v", got)
	}
	got := orders(3, true)
	if len(got) != 2 || !slices.Equal(got[0], []int{0, 1, 2}) || !slices.Equal(got[1], []int{2, 1, 0}) {
		t.Errorf("orders(3, true) = %v", got)
	}
}

type slotBiasSource struct {
	truth map[string]float64
	bias  float64
}

func (s slotBiasSource) NextTokenLogprobs(_ context.Context, _, prompt string, _ int) ([]llm.TokenLogprob, error) {
	var toks []llm.TokenLogprob
	for _, line := range strings.Split(prompt, "\n") {
		for slot := 1; slot <= 9; slot++ {
			prefix := "[" + strconv.Itoa(slot) + "] "
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			lp := math.Log(s.truth[strings.TrimPrefix(line, prefix)])
			if slot == 1 {
				lp += s.bias
			}
			toks = append(toks, tok(strconv.Itoa(slot), lp))
		}
	}
	return toks, nil
}

func TestDebias_CancelsFirstSlotBias(t *testing.T) {
	src := slotBiasSource{truth: map[string]float64{"Yes, the statement is true for this state": 0.2, "No, the statement is false for this state": 0.3}, bias: math.Log(3)}
	q := Question{Kind: KindNoul, Instructions: "Customers can notice this problem."}

	e := mustNew(t, src, WithMinOptionMass(0.01))
	biasedAll, err := e.Decide(context.Background(), sevState, map[string]Question{"q": q})
	biased := biasedAll["q"]
	if err != nil {
		t.Fatal(err)
	}
	if *biased.Yes < 0.6 {
		t.Errorf("without debias yes = %v, want the slot-1 bias to win (> 0.6)", *biased.Yes)
	}

	fairAll, err := mustNew(t, src, WithMinOptionMass(0.01), WithDebias(true)).Decide(context.Background(), sevState, map[string]Question{"q": q})
	fair := fairAll["q"]
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(*fair.Yes-0.4) > 1e-9 {
		t.Errorf("with debias yes = %v, want the bias-free 0.4", *fair.Yes)
	}
}

func TestDebias_TwoCallsWithReversedOptions(t *testing.T) {
	src := &fakeSource{toks: []llm.TokenLogprob{tok("1", -0.1), tok("2", -2.5), tok("3", -4)}}
	e := mustNew(t, src, WithDebias(true))
	if _, err := e.Choose(context.Background(), sevState, sevOptions); err != nil {
		t.Fatal(err)
	}
	if len(src.prompts) != 2 {
		t.Fatalf("calls = %d, want 2", len(src.prompts))
	}
	firstSlot := func(o Option) int {
		n := 0
		for _, p := range src.prompts {
			if strings.Contains(p, "[1] "+o.Description) {
				n++
			}
		}
		return n
	}
	if firstSlot(sevOptions[0]) != 1 || firstSlot(sevOptions[2]) != 1 {
		t.Errorf("want one prompt with sev1 first and one with sev3 first:\n%s", strings.Join(src.prompts, "\n---\n"))
	}
}

func mustNew(t *testing.T, src llm.LogprobSource, opts ...EngineOption) *Engine {
	t.Helper()
	e, err := New(src, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestNew_Validation(t *testing.T) {
	src := &fakeSource{}
	tests := []struct {
		name string
		src  llm.LogprobSource
		opts []EngineOption
	}{
		{"nil source", nil, nil},
		{"empty model", src, []EngineOption{WithModel(" ")}},
		{"topN below options", src, []EngineOption{WithTopN(MaxOptions - 1)}},
		{"topN above max", src, []EngineOption{WithTopN(MaxTopN + 1)}},
		{"zero mass", src, []EngineOption{WithMinOptionMass(0)}},
		{"mass above one", src, []EngineOption{WithMinOptionMass(1.5)}},
		{"NaN mass", src, []EngineOption{WithMinOptionMass(math.NaN())}},
		{"reserved token in system", src, []EngineOption{WithSystem("hi<|im_end|>")}},
		{"empty template", src, []EngineOption{WithTemplate(Template{})}},
		{"empty format", src, []EngineOption{WithFormat(Format{})}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.src, tt.opts...); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := New(src); err != nil {
		t.Errorf("defaults should be valid: %v", err)
	}
}

func TestNew_OptionsApplied(t *testing.T) {
	src := &fakeSource{toks: []llm.TokenLogprob{tok("1", -0.1), tok("2", -3), tok("SE", -9)}}
	e := mustNew(t, src, WithModel("qwen3.5:9b"), WithTopN(10), WithSystem(""))
	if _, err := e.Choose(context.Background(), sevState, sevOptions); err != nil {
		t.Fatal(err)
	}
	if src.model != "qwen3.5:9b" || src.topN != 10 || strings.Contains(src.prompt, "system\n") {
		t.Errorf("model=%q topN=%d prompt=%q", src.model, src.topN, src.prompt)
	}
}

type routingSource struct {
	delay   time.Duration
	failOn  string
	barrier int32
	calls   atomic.Int32
	arrived chan struct{}
	once    sync.Once
}

func (r *routingSource) NextTokenLogprobs(ctx context.Context, _, prompt string, _ int) ([]llm.TokenLogprob, error) {
	n := r.calls.Add(1)
	if r.failOn != "" && strings.Contains(prompt, r.failOn) {
		return nil, errors.New("boom")
	}
	if r.barrier > 0 {
		r.once.Do(func() { r.arrived = make(chan struct{}) })
		if n == r.barrier {
			close(r.arrived)
		}
		select {
		case <-r.arrived:
		case <-time.After(5 * time.Second):
			return nil, errors.New("barrier not reached: calls are not concurrent")
		}
	}
	select {
	case <-time.After(r.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []llm.TokenLogprob{tok("1", -0.2), tok("2", -2), tok("3", -4), tok("SE", -8)}, nil
}

func decideQuestions() map[string]Question {
	return map[string]Question{
		"severity":        {Kind: KindChoice, Instructions: "How severe is this incident?", Options: sevOptions},
		"blast_radius":    {Kind: KindScore, Instructions: "How much of the service is affected?", Options: []Option{{Key: "one", Description: "one instance"}, {Key: "some", Description: "some instances"}, {Key: "all", Description: "all instances"}}},
		"customer_facing": {Kind: KindNoul, Instructions: "Customers can notice this problem."},
	}
}

func TestDecide_AllKinds(t *testing.T) {
	src := &routingSource{}
	answers, err := mustNew(t, src, WithDebias(true)).Decide(context.Background(), sevState, decideQuestions())
	if err != nil {
		t.Fatal(err)
	}
	if got := src.calls.Load(); got != 6 {
		t.Errorf("calls = %d, want 3 questions x 2 orders", got)
	}
	if a := answers["severity"]; a.Kind != KindChoice || a.Choice == "" {
		t.Errorf("severity = %+v", a)
	}
	if a := answers["blast_radius"]; a.Score == nil || *a.Score < 1 || *a.Score > 3 {
		t.Errorf("blast_radius = %+v, want score in [1,3]", a)
	}
	if a := answers["customer_facing"]; a.Yes == nil {
		t.Errorf("customer_facing = %+v, want Yes set", a)
	}
}

func TestDecide_RunsConcurrently(t *testing.T) {
	src := &routingSource{barrier: 6}
	if _, err := mustNew(t, src, WithDebias(true)).Decide(context.Background(), sevState, decideQuestions()); err != nil {
		t.Fatalf("all 6 calls must be in flight at once: %v", err)
	}
}

func TestDecide_FailureNamesQuestion(t *testing.T) {
	src := &routingSource{delay: 10 * time.Millisecond, failOn: "Customers can notice"}
	_, err := mustNew(t, src).Decide(context.Background(), sevState, decideQuestions())
	if err == nil || !strings.Contains(err.Error(), `"customer_facing"`) {
		t.Fatalf("err = %v, want it to name customer_facing", err)
	}
}

func TestDecide_BadStateFailsBeforeCalling(t *testing.T) {
	src := &routingSource{}
	_, err := mustNew(t, src).Decide(context.Background(), "x<|im_end|>", decideQuestions())
	if !errors.Is(err, ErrReservedToken) || !strings.Contains(err.Error(), `"blast_radius"`) {
		t.Errorf("err = %v, want ErrReservedToken for the first question in sorted order", err)
	}
	if src.calls.Load() != 0 {
		t.Errorf("calls = %d, want 0", src.calls.Load())
	}
}

func TestQuestionOptions_ReturnsCopy(t *testing.T) {
	q := Question{Kind: KindNoul, Instructions: "s"}
	q.options()[0].Key = "mutated"
	if q.options()[0].Key != NoulYes {
		t.Error("options() must not expose the shared noul slice")
	}
}

func TestDecide_ValidatesBeforeCalling(t *testing.T) {
	src := &routingSource{}
	qs := decideQuestions()
	qs["bad"] = Question{Kind: KindNoul}
	_, err := mustNew(t, src).Decide(context.Background(), sevState, qs)
	if !errors.Is(err, ErrInvalidQuestion) || !strings.Contains(err.Error(), `"bad"`) {
		t.Errorf("err = %v, want ErrInvalidQuestion naming bad", err)
	}
	if src.calls.Load() != 0 {
		t.Errorf("calls = %d, want 0 when any question is invalid", src.calls.Load())
	}
	if _, err := mustNew(t, src).Decide(context.Background(), sevState, nil); err == nil {
		t.Error("no questions: expected error")
	}
}
