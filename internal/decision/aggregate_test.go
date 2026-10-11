package decision

import (
	"errors"
	"math"
	"testing"
)

func lps(ps ...float64) []float64 {
	out := make([]float64, len(ps))
	for i, p := range ps {
		if p == 0 {
			out[i] = math.Inf(-1)
			continue
		}
		out[i] = math.Log(p)
	}
	return out
}

var abc = Question{Kind: KindChoice, Instructions: "q", Options: []Option{
	{Key: "a", Description: "a"}, {Key: "b", Description: "b"}, {Key: "c", Description: "c"},
}}

func TestFinalize_SingleRunMatchesSoftmax(t *testing.T) {
	ans, err := finalize(abc, []raw{{order: []int{0, 1, 2}, logprobs: lps(0.6, 0.3, 0.1), floor: -9, hasFloor: true, mass: 1}}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]float64{"a": 0.6, "b": 0.3, "c": 0.1} {
		if math.Abs(ans.Probs[k]-want) > eps {
			t.Errorf("p(%s) = %v, want %v", k, ans.Probs[k], want)
		}
	}
	if ans.Choice != "a" || ans.Score != nil || ans.Yes != nil {
		t.Errorf("ans = %+v, want choice a and no score/yes", ans)
	}
}

func TestFinalize_ReversedRunMapsSlotsToKeys(t *testing.T) {
	rev := raw{order: []int{2, 1, 0}, logprobs: lps(0.1, 0.3, 0.6), floor: -9, hasFloor: true, mass: 1}
	ans, err := finalize(abc, []raw{rev}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(ans.Probs["a"]-0.6) > eps || math.Abs(ans.Probs["c"]-0.1) > eps {
		t.Errorf("probs = %v, want a=0.6 c=0.1 after mapping slots back", ans.Probs)
	}
}

func TestFinalize_GeometricMean(t *testing.T) {
	fwd := raw{order: []int{0, 1, 2}, logprobs: lps(0.8, 0.15, 0.05), floor: -9, hasFloor: true, mass: 1}
	rev := raw{order: []int{2, 1, 0}, logprobs: lps(0.1, 0.6, 0.3), floor: -9, hasFloor: true, mass: 1}
	ans, err := finalize(abc, []raw{fwd, rev}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	ga, gb, gc := math.Sqrt(0.8*0.3), math.Sqrt(0.15*0.6), math.Sqrt(0.05*0.1)
	z := ga + gb + gc
	for k, want := range map[string]float64{"a": ga / z, "b": gb / z, "c": gc / z} {
		if math.Abs(ans.Probs[k]-want) > eps {
			t.Errorf("p(%s) = %v, want %v", k, ans.Probs[k], want)
		}
	}
}

func TestFinalize_RenormalizesEachRun(t *testing.T) {
	full := raw{order: []int{0, 1, 2}, logprobs: lps(0.6, 0.3, 0.1), floor: -9, hasFloor: true, mass: 1}
	half := raw{order: []int{0, 1, 2}, logprobs: lps(0.3, 0.15, 0.05), floor: -9, hasFloor: true, mass: 0.5}
	ans, err := finalize(abc, []raw{full, half}, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(ans.Probs["a"]-0.6) > eps {
		t.Errorf("p(a) = %v, want 0.6: runs with less mass must not be down-weighted", ans.Probs["a"])
	}
}

func TestFinalize_FloorPreventsVeto(t *testing.T) {
	fwd := raw{order: []int{0, 1, 2}, logprobs: lps(0.5, 0.4, 0.1), floor: math.Log(0.01), hasFloor: true, mass: 1}
	rev := raw{order: []int{2, 1, 0}, logprobs: lps(0.2, 0, 0.7), floor: math.Log(0.01), hasFloor: true, mass: 0.9}
	ans, err := finalize(abc, []raw{fwd, rev}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Probs["b"] < 0.05 {
		t.Errorf("p(b) = %v: missing in one order must not veto an option the other order rated 0.4", ans.Probs["b"])
	}
	if len(ans.Missing) != 0 {
		t.Errorf("missing = %v, want none (b appeared in the forward run)", ans.Missing)
	}
}

func TestFinalize_MissingInAllOrders(t *testing.T) {
	fwd := raw{order: []int{0, 1, 2}, logprobs: lps(0.9, 0, 0.1), floor: -9, hasFloor: true, mass: 1}
	rev := raw{order: []int{2, 1, 0}, logprobs: lps(0.1, 0, 0.9), floor: -9, hasFloor: true, mass: 1}
	ans, err := finalize(abc, []raw{fwd, rev}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Missing) != 1 || ans.Missing[0] != "b" {
		t.Errorf("missing = %v, want [b]", ans.Missing)
	}
	if ans.Probs["b"] <= 0 || ans.Probs["b"] > 1e-3 {
		t.Errorf("p(b) = %v, want tiny but non-zero (floor)", ans.Probs["b"])
	}
}

func TestFinalize_MinMassAcrossRuns(t *testing.T) {
	good := raw{order: []int{0, 1, 2}, logprobs: lps(0.6, 0.3, 0.1), floor: -9, hasFloor: true, mass: 0.99}
	bad := raw{order: []int{2, 1, 0}, logprobs: lps(0.1, 0.1, 0.1), floor: -9, hasFloor: true, mass: 0.3}
	ans, err := finalize(abc, []raw{good, bad}, 0.5)
	if !errors.Is(err, ErrLowOptionMass) {
		t.Errorf("err = %v (mass %v), want ErrLowOptionMass from the weakest run", err, ans.OptionMass)
	}
}

func TestFinalize_ScoreExpectedValue(t *testing.T) {
	q := Question{Kind: KindScore, Instructions: "q", Options: abc.Options}
	ans, err := finalize(q, []raw{{order: []int{0, 1, 2}, logprobs: lps(0.1, 0.2, 0.7), floor: -9, hasFloor: true, mass: 1}}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Score == nil || math.Abs(*ans.Score-2.6) > eps {
		t.Fatalf("score = %v, want 2.6", ans.Score)
	}
	if ans.Yes != nil {
		t.Error("score answer must not set Yes")
	}
}

func TestFinalize_NoulYes(t *testing.T) {
	q := Question{Kind: KindNoul, Instructions: "s"}
	rev := raw{order: []int{1, 0}, logprobs: lps(0.25, 0.75), floor: -9, hasFloor: true, mass: 1}
	ans, err := finalize(q, []raw{rev}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Yes == nil || math.Abs(*ans.Yes-0.75) > eps {
		t.Fatalf("yes = %v, want 0.75 (yes was in slot 2)", ans.Yes)
	}
	if ans.Score != nil {
		t.Error("noul answer must not set Score")
	}
}

func TestFinalize_Errors(t *testing.T) {
	if _, err := finalize(abc, nil, 0.5); err == nil {
		t.Error("no runs: expected error")
	}
	short := raw{order: []int{0, 1}, logprobs: lps(0.5, 0.5), floor: -9, hasFloor: true, mass: 1}
	if _, err := finalize(abc, []raw{short}, 0.5); err == nil {
		t.Error("slot count mismatch: expected error")
	}
}

func TestFinalize_NoFloorErrors(t *testing.T) {
	r := raw{order: []int{0, 1, 2}, logprobs: lps(0.9, 0, 0.1), floor: math.Log(0.1), hasFloor: false, mass: 1}
	if _, err := finalize(abc, []raw{r}, 0.5); !errors.Is(err, ErrNoFloor) {
		t.Errorf("err = %v, want ErrNoFloor", err)
	}
	r.logprobs = lps(0.5, 0.4, 0.1)
	if _, err := finalize(abc, []raw{r}, 0.5); err != nil {
		t.Errorf("no missing options: floor not needed, got %v", err)
	}
}

func TestFinalize_TwoOptionChoice(t *testing.T) {
	q := Question{Kind: KindChoice, Instructions: "q", Options: abc.Options[:2]}
	ans, err := finalize(q, []raw{{order: []int{0, 1}, logprobs: lps(0.3, 0.7), floor: -9, hasFloor: true, mass: 1}}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Choice != "b" || math.Abs(ans.Margin-0.4) > eps {
		t.Errorf("choice=%q margin=%v, want b and 0.4", ans.Choice, ans.Margin)
	}
}

func TestFinalize_MassAtThresholdPasses(t *testing.T) {
	r := raw{order: []int{0, 1, 2}, logprobs: lps(0.3, 0.15, 0.05), floor: -9, hasFloor: true, mass: 0.5}
	if _, err := finalize(abc, []raw{r}, 0.5); err != nil {
		t.Errorf("mass == threshold should pass, got %v", err)
	}
}

func TestFinalize_NaN(t *testing.T) {
	r := raw{order: []int{0, 1, 2}, logprobs: []float64{math.NaN(), -1, -2}, floor: -9, hasFloor: true, mass: 1}
	if _, err := finalize(abc, []raw{r}, 0.5); !errors.Is(err, ErrNonFinite) {
		t.Errorf("err = %v, want ErrNonFinite", err)
	}
}

func TestFinalize_ScoreReversedOrder(t *testing.T) {
	q := Question{Kind: KindScore, Instructions: "q", Options: abc.Options}
	rev := raw{order: []int{2, 1, 0}, logprobs: lps(0.7, 0.2, 0.1), floor: -9, hasFloor: true, mass: 1}
	ans, err := finalize(q, []raw{rev}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Score == nil || math.Abs(*ans.Score-2.6) > eps {
		t.Errorf("score = %v, want 2.6: level 3 sat in slot 1", ans.Score)
	}
}

func TestFinalize_RejectsBadOrder(t *testing.T) {
	for _, order := range [][]int{{0, 0, 1}, {0, 1, 3}, {-1, 0, 1}} {
		r := raw{order: order, logprobs: lps(0.5, 0.3, 0.2), floor: -9, hasFloor: true, mass: 1}
		if _, err := finalize(abc, []raw{r}, 0.5); err == nil {
			t.Errorf("order %v: expected error", order)
		}
	}
}
