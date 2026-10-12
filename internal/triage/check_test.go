package triage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shadi/triage-logprobe/internal/decision"
	"github.com/shadi/triage-logprobe/internal/metrics"
)

func TestCheck(t *testing.T) {
	r, err := DefaultRubric()
	if err != nil {
		t.Fatal(err)
	}
	e, err := decision.New(&fakeLLM{}, decision.WithSystem(r.System))
	if err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{
		now:    map[string][]metrics.Series{"q": {series(nil, 0.3)}},
		before: map[string][]metrics.Series{"q": {series(nil, 0, 0, 0)}},
	}
	p := Params{At: at.In(time.FixedZone("x", 3600))}
	res, err := Check(context.Background(), src, e, r, []Metric{{Name: "error_5xx_ratio", Query: "q"}}, p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Severity != "SEV2" || res.Answer.Choice != "sev2" {
		t.Errorf("severity = %q choice = %q, want SEV2", res.Severity, res.Answer.Choice)
	}
	if !strings.Contains(res.State, "error_5xx_ratio: 30%") || len(res.Readings) != 1 {
		t.Errorf("state = %q readings = %d", res.State, len(res.Readings))
	}
	if res.At != at {
		t.Errorf("at = %v, want %v in UTC", res.At, at)
	}
	if _, ok := res.LatencyMS["model"]; !ok {
		t.Error("missing model latency")
	}

	src.err = errors.New("boom")
	if _, err := Check(context.Background(), src, e, r, []Metric{{Name: "m", Query: "q"}}, p); err == nil {
		t.Error("metrics error: expected error")
	}
}
