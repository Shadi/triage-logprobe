package decision

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/shadi/triage-logprobs/internal/llm/ollama"
)

func integrationEngine(t *testing.T, opts ...EngineOption) *Engine {
	t.Helper()
	if os.Getenv("LOGPROBS_IT") != "1" {
		t.Skip("set LOGPROBS_IT=1 to run against a local Ollama")
	}
	model := os.Getenv("LOGPROBS_MODEL")
	if model == "" {
		model = DefaultModel
	}
	return mustNew(t, ollama.New(os.Getenv("OLLAMA_HOST_URL")), append([]EngineOption{WithModel(model)}, opts...)...)
}

func TestIntegration_Deterministic(t *testing.T) {
	e := integrationEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	state := `2026-10-11T10:42:07Z ERROR payment-svc: upstream connect error, reset reason: connection timeout (stripe-gw:443)`
	opts := []Option{
		{Key: "error", Description: "The log line reports an error"},
		{Key: "not_error", Description: "The log line is informational, not an error"},
	}

	var first Answer
	for i := range 5 {
		res, err := e.Choose(ctx, state, opts)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("run %d: %+v", i, res)
		if res.Choice != "error" {
			t.Errorf("run %d: choice = %q, want error", i, res.Choice)
		}
		if i == 0 {
			first = res
			continue
		}
		for k, p := range res.Probs {
			if math.Abs(p-first.Probs[k]) > 1e-2 {
				t.Errorf("run %d: p(%s) = %.5f, first run %.5f", i, k, p, first.Probs[k])
			}
		}
	}
}

func TestIntegration_SevState(t *testing.T) {
	e := integrationEngine(t, WithSystem("You are an incident triage decision model."))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := e.Choose(ctx, sevState, sevOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", res)
	if res.Choice != "sev1" {
		t.Errorf("choice = %q, want sev1", res.Choice)
	}
}
