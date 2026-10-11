package ollama

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIntegration_NextTokenLogprobs(t *testing.T) {
	if os.Getenv("LOGPROBE_IT") != "1" {
		t.Skip("set LOGPROBE_IT=1 to run against a local Ollama")
	}
	model := os.Getenv("LOGPROBE_MODEL")
	if model == "" {
		model = "qwen3.5:4b"
	}
	prompt := "<|im_start|>user\nState: disk 99% full on db-1\nOptions:\n[1] urgent\n[2] not urgent\nSelect one option. Answer only with its bracketed numeric identifier.<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\nBest answer: ["

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	toks, err := New(os.Getenv("OLLAMA_HOST_URL")).NextTokenLogprobs(ctx, model, prompt, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) == 0 {
		t.Fatal("no tokens returned")
	}
	seen := map[string]bool{}
	for _, tk := range toks {
		t.Logf("%q %.4f %v", tk.Token, tk.Logprob, tk.Bytes)
		if tk.Logprob > 0 {
			t.Errorf("logprob for %q = %f, want <= 0", tk.Token, tk.Logprob)
		}
		seen[tk.Token] = true
	}
	if !seen["1"] || !seen["2"] {
		t.Errorf("want both option digits in top tokens, got %v", seen)
	}
	if toks[0].Token != "1" && toks[0].Token != "2" {
		t.Errorf("top token = %q, want an option digit", toks[0].Token)
	}
}
