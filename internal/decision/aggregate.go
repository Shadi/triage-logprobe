package decision

import (
	"errors"
	"fmt"
	"math"
)

var ErrNoFloor = errors.New("decision: an option is missing and the response has no non-option tokens to bound it")

type raw struct {
	order    []int
	logprobs []float64
	floor    float64
	hasFloor bool
	mass     float64
}

func isPermutation(order []int, k int) bool {
	if len(order) != k {
		return false
	}
	seen := make([]bool, k)
	for _, i := range order {
		if i < 0 || i >= k || seen[i] {
			return false
		}
		seen[i] = true
	}
	return true
}

func finalize(q Question, runs []raw, minMass float64) (Answer, error) {
	opts := q.options()
	k := len(opts)
	if len(runs) == 0 {
		return Answer{}, fmt.Errorf("decision: no runs to combine")
	}

	sum := make([]float64, k)
	seen := make([]bool, k)
	mass := 1.0
	for _, r := range runs {
		if len(r.logprobs) != k || !isPermutation(r.order, k) {
			return Answer{}, fmt.Errorf("decision: run order %v does not match %d options", r.order, k)
		}
		mass = math.Min(mass, r.mass)
		byOption := make([]float64, k)
		for slot, idx := range r.order {
			lp := r.logprobs[slot]
			if math.IsInf(lp, -1) {
				if !r.hasFloor {
					return Answer{}, ErrNoFloor
				}
				lp = r.floor
			} else {
				seen[idx] = true
			}
			byOption[idx] = lp
		}
		norm := logSumExp(byOption)
		for i, lp := range byOption {
			sum[i] += lp - norm
		}
	}

	if mass < minMass {
		return Answer{}, fmt.Errorf("%w: %.3f < %.3f", ErrLowOptionMass, mass, minMass)
	}

	avg := make([]float64, k)
	for i := range sum {
		avg[i] = sum[i] / float64(len(runs))
	}
	probs := Softmax(avg)

	a := Answer{
		Kind:       q.Kind,
		Probs:      make(map[string]float64, k),
		Confidence: Confidence(probs),
		OptionMass: mass,
	}
	best, second := -1, -1
	var expected float64
	for i, o := range opts {
		if math.IsNaN(probs[i]) {
			return Answer{}, ErrNonFinite
		}
		a.Probs[o.Key] = probs[i]
		expected += probs[i] * float64(i+1)
		if !seen[i] {
			a.Missing = append(a.Missing, o.Key)
		}
		switch {
		case best < 0 || probs[i] > probs[best]:
			best, second = i, best
		case second < 0 || probs[i] > probs[second]:
			second = i
		}
	}
	a.Choice = opts[best].Key
	a.Margin = probs[best] - probs[second]

	switch q.Kind {
	case KindScore:
		a.Score = &expected
	case KindNoul:
		yes := a.Probs[NoulYes]
		a.Yes = &yes
	}
	return a, nil
}

func logSumExp(xs []float64) float64 {
	acc := math.Inf(-1)
	for _, x := range xs {
		acc = logAddExp(acc, x)
	}
	return acc
}
