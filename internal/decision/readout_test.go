package decision

import (
	"errors"
	"math"
	"testing"

	"github.com/shadi/triage-logprobe/internal/llm"
)

const eps = 1e-9

func tok(s string, lp float64) llm.TokenLogprob {
	return llm.TokenLogprob{Token: s, Logprob: lp, Bytes: []byte(s)}
}

func TestOptionLogprobs_Basic(t *testing.T) {
	toks := []llm.TokenLogprob{tok("1", -0.427), tok("2", -1.146), tok("SE", -6), tok("3", -3.73), tok("4", -5)}
	lp, err := OptionLogprobs(toks, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{-0.427, -1.146, -3.73}
	for i := range want {
		if math.Abs(lp[i]-want[i]) > eps {
			t.Errorf("lp[%d] = %v, want %v", i, lp[i], want[i])
		}
	}

	p := Softmax(lp)
	z := math.Exp(-0.427) + math.Exp(-1.146) + math.Exp(-3.73)
	for i, w := range want {
		if math.Abs(p[i]-math.Exp(w)/z) > eps {
			t.Errorf("p[%d] = %v, want %v", i, p[i], math.Exp(w)/z)
		}
	}
}

func TestOptionLogprobs_MergesSpellings(t *testing.T) {
	toks := []llm.TokenLogprob{tok("1", math.Log(0.3)), tok(" 1", math.Log(0.2)), tok("1]", math.Log(0.1)), tok("2", math.Log(0.4))}
	lp, err := OptionLogprobs(toks, 2)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(math.Exp(lp[0])-0.6) > eps {
		t.Errorf("merged p(1) = %v, want 0.6", math.Exp(lp[0]))
	}
}

func TestOptionLogprobs_PrefersBytesOverToken(t *testing.T) {
	toks := []llm.TokenLogprob{{Token: "3", Logprob: -0.1, Bytes: []byte("1")}, tok("2", -2)}
	lp, err := OptionLogprobs(toks, 3)
	if err != nil {
		t.Fatal(err)
	}
	if lp[0] != -0.1 || !math.IsInf(lp[2], -1) {
		t.Errorf("lp = %v, want bytes \"1\" to win over token \"3\"", lp)
	}
}

func TestOptionLogprobs_MissingOption(t *testing.T) {
	lp, err := OptionLogprobs([]llm.TokenLogprob{tok("1", -0.2), tok("3", -2)}, 3)
	if err != nil {
		t.Fatal(err)
	}
	p := Softmax(lp)
	if p[1] != 0 {
		t.Errorf("missing option p = %v, want 0", p[1])
	}
}

func TestOptionLogprobs_Errors(t *testing.T) {
	if _, err := OptionLogprobs([]llm.TokenLogprob{tok("[", -0.1), tok("</think>", -1)}, 3); !errors.Is(err, ErrNoOptionTokens) {
		t.Errorf("all missing: err = %v, want ErrNoOptionTokens", err)
	}
	if _, err := OptionLogprobs([]llm.TokenLogprob{tok("4", -0.1)}, 3); !errors.Is(err, ErrNoOptionTokens) {
		t.Errorf("out-of-range digit only: err = %v, want ErrNoOptionTokens", err)
	}
	for _, k := range []int{0, 1, 10} {
		if _, err := OptionLogprobs([]llm.TokenLogprob{tok("1", -0.1)}, k); err == nil {
			t.Errorf("k=%d: expected error", k)
		}
	}
}

func TestOptionLogprobs_NonFinite(t *testing.T) {
	toks := []llm.TokenLogprob{tok("1", math.Inf(1)), tok("2", math.NaN()), tok("3", -1)}
	lp, err := OptionLogprobs(toks, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsInf(lp[0], -1) || !math.IsInf(lp[1], -1) || lp[2] != -1 {
		t.Errorf("lp = %v, want +Inf and NaN ignored", lp)
	}
	if _, err := OptionLogprobs([]llm.TokenLogprob{tok("1", math.Inf(1))}, 2); !errors.Is(err, ErrNoOptionTokens) {
		t.Errorf("only +Inf: err = %v, want ErrNoOptionTokens", err)
	}
}

func TestOptionLogprobs_ClampsPositive(t *testing.T) {
	lp, err := OptionLogprobs([]llm.TokenLogprob{tok("1", 1e-7), tok("2", -5)}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if lp[0] != 0 {
		t.Errorf("lp[0] = %v, want clamped to 0", lp[0])
	}
}

func TestOptionLogprobs_TokenShapes(t *testing.T) {
	tests := []struct {
		text string
		k    int
		pos  int
	}{
		{" 1]", 3, 0},
		{"9", 9, 8},
		{"1]]", 3, -1},
		{"9", 8, -1},
		{"０", 3, -1},
		{"+1", 3, -1},
		{"10", 9, -1},
	}
	for _, tt := range tests {
		lp, err := OptionLogprobs([]llm.TokenLogprob{tok(tt.text, -0.1)}, tt.k)
		if tt.pos < 0 {
			if !errors.Is(err, ErrNoOptionTokens) {
				t.Errorf("%q k=%d: err = %v, want ErrNoOptionTokens", tt.text, tt.k, err)
			}
			continue
		}
		if err != nil || lp[tt.pos] != -0.1 {
			t.Errorf("%q k=%d: lp = %v err = %v, want position %d", tt.text, tt.k, lp, err, tt.pos)
		}
	}
}

func TestTokenText_FallsBackToToken(t *testing.T) {
	lp, err := OptionLogprobs([]llm.TokenLogprob{{Token: "2", Logprob: -0.1}}, 2)
	if err != nil || lp[1] != -0.1 {
		t.Errorf("lp = %v err = %v, want Token used when Bytes is empty", lp, err)
	}
}

func TestSoftmax_LargeNegative(t *testing.T) {
	p := Softmax([]float64{-1000, -1001, math.Inf(-1)})
	if math.IsNaN(p[0]) || math.IsNaN(p[1]) {
		t.Fatalf("NaN in %v", p)
	}
	if math.Abs(p[0]+p[1]-1) > eps || p[2] != 0 {
		t.Errorf("p = %v, want two-way split summing to 1 and 0", p)
	}
}

func TestOptionMass(t *testing.T) {
	if m := OptionMass([]float64{math.Log(0.5), math.Log(0.2), math.Inf(-1)}); math.Abs(m-0.7) > eps {
		t.Errorf("mass = %v, want 0.7", m)
	}
}

func TestConfidence(t *testing.T) {
	tests := []struct {
		p    []float64
		want float64
	}{
		{[]float64{1, 0, 0}, 1},
		{[]float64{1.0 / 3, 1.0 / 3, 1.0 / 3}, 0},
		{[]float64{0.5, 0.5}, 0},
	}
	for _, tt := range tests {
		if got := Confidence(tt.p); math.Abs(got-tt.want) > eps {
			t.Errorf("Confidence(%v) = %v, want %v", tt.p, got, tt.want)
		}
	}
	want := 1 - (-(0.9*math.Log(0.9) + 0.1*math.Log(0.1)))/math.Log(2)
	if c := Confidence([]float64{0.9, 0.1}); math.Abs(c-want) > eps {
		t.Errorf("Confidence(0.9,0.1) = %v, want %v", c, want)
	}
	if c := Confidence([]float64{0.8, 0.15, 0.05}); c <= 0 || c >= 1 {
		t.Errorf("Confidence of a skewed distribution = %v, want in (0,1)", c)
	}
}
