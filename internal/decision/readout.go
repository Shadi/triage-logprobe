package decision

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/shadi/triage-logprobs/internal/llm"
)

var ErrNoOptionTokens = errors.New("decision: no option IDs among the top tokens")

func OptionLogprobs(toks []llm.TokenLogprob, k int) ([]float64, error) {
	if k < MinOptions || k > MaxOptions {
		return nil, fmt.Errorf("decision: k must be between %d and %d, got %d", MinOptions, MaxOptions, k)
	}
	lp := make([]float64, k)
	for i := range lp {
		lp[i] = math.Inf(-1)
	}
	found := false
	for _, t := range toks {
		pos, ok := optionIndex(t.Text(), k)
		if !ok || math.IsNaN(t.Logprob) || math.IsInf(t.Logprob, 0) {
			continue
		}
		lp[pos] = logAddExp(lp[pos], math.Min(t.Logprob, 0))
		found = true
	}
	if !found {
		return nil, ErrNoOptionTokens
	}
	return lp, nil
}

func optionIndex(text string, k int) (int, bool) {
	s := strings.TrimSuffix(strings.TrimSpace(text), "]")
	if len(s) != 1 {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > k {
		return 0, false
	}
	return n - 1, true
}

func logAddExp(a, b float64) float64 {
	if math.IsInf(a, -1) {
		return b
	}
	if math.IsInf(b, -1) {
		return a
	}
	m := math.Max(a, b)
	return m + math.Log(math.Exp(a-m)+math.Exp(b-m))
}

func Softmax(logits []float64) []float64 {
	m := math.Inf(-1)
	for _, v := range logits {
		m = math.Max(m, v)
	}
	out := make([]float64, len(logits))
	if math.IsInf(m, -1) {
		return out
	}
	var sum float64
	for i, v := range logits {
		out[i] = math.Exp(v - m)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

func OptionMass(logprobs []float64) float64 {
	var sum float64
	for _, v := range logprobs {
		sum += math.Exp(v)
	}
	return math.Min(sum, 1)
}

func Confidence(probs []float64) float64 {
	if len(probs) < 2 {
		return 1
	}
	var h float64
	for _, p := range probs {
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	return math.Max(0, 1-h/math.Log(float64(len(probs))))
}
