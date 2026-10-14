package triage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadi/triage-logprobe/internal/decision"
	"github.com/shadi/triage-logprobe/internal/llm"
)

func TestDefaultRubric(t *testing.T) {
	r, err := DefaultRubric()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Options) != 4 || r.Options[0].Key != "sev1" || r.Options[3].Key != "healthy" || r.System == "" {
		t.Errorf("rubric = %+v", r)
	}
}

func TestLoadRubric(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok := write("ok.json", `{"system":"s","question":"q","options":[{"key":"a","description":"A"},{"key":"b","description":"B"}]}`)
	if r, err := LoadRubric(ok); err != nil || r.Options[1].Description != "B" {
		t.Errorf("r=%+v err=%v", r, err)
	}
	for name, body := range map[string]string{
		"one.json":      `{"question":"q","options":[{"key":"a","description":"A"}]}`,
		"noq.json":      `{"options":[{"key":"a","description":"A"},{"key":"b","description":"B"}]}`,
		"trailing.json": `{"system":"s","question":"q","options":[{"key":"a","description":"A"},{"key":"b","description":"B"}]} {}`,
	} {
		if _, err := LoadRubric(write(name, body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

type fakeLLM struct{ prompt string }

func (f *fakeLLM) NextTokenLogprobs(_ context.Context, _, prompt string, _ int) ([]llm.TokenLogprob, error) {
	f.prompt = prompt
	return []llm.TokenLogprob{
		{Token: "2", Logprob: -0.2}, {Token: "1", Logprob: -2}, {Token: "3", Logprob: -3}, {Token: "SE", Logprob: -8},
	}, nil
}

func TestAssess(t *testing.T) {
	r, err := DefaultRubric()
	if err != nil {
		t.Fatal(err)
	}
	src := &fakeLLM{}
	e, err := decision.New(src, decision.WithSystem(r.System))
	if err != nil {
		t.Fatal(err)
	}
	ans, err := Assess(context.Background(), e, r, "error_5xx_ratio: 33.4% (normal 0.1%)")
	if err != nil {
		t.Fatal(err)
	}
	if ans.Choice != "sev2" {
		t.Errorf("choice = %q, want sev2", ans.Choice)
	}
	for _, want := range []string{r.System, "Question: " + r.Question, "error_5xx_ratio: 33.4%"} {
		if !strings.Contains(src.prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
