package triage

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shadi/triage-logprobe/internal/metrics"
)

type call struct {
	q          string
	at         time.Time
	start, end time.Time
	step       time.Duration
}

type fakeSource struct {
	mu     sync.Mutex
	now    map[string][]metrics.Series
	before map[string][]metrics.Series
	err    error
	calls  []call
}

func (f *fakeSource) Query(_ context.Context, q string, at time.Time) ([]metrics.Series, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{q: q, at: at})
	return f.now[q], f.err
}

func (f *fakeSource) QueryRange(_ context.Context, q string, start, end time.Time, step time.Duration) ([]metrics.Series, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{q: q, start: start, end: end, step: step})
	return f.before[q], f.err
}

func series(labels map[string]string, vs ...float64) metrics.Series {
	s := metrics.Series{Labels: labels}
	for i, v := range vs {
		s.Samples = append(s.Samples, metrics.Sample{T: time.Unix(int64(i*60), 0), V: v})
	}
	return s
}

var at = time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)

func TestCollect_WindowsAndMedian(t *testing.T) {
	src := &fakeSource{
		now:    map[string][]metrics.Series{"q": {series(map[string]string{"__name__": "x", "pod": "a"}, 9)}},
		before: map[string][]metrics.Series{"q": {series(map[string]string{"__name__": "x", "pod": "a"}, 1, 2, math.NaN(), 9)}},
	}
	rs, err := Collect(context.Background(), src, []Metric{{Name: "m", Query: "q"}}, at, 5*time.Minute, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := rs[0].Series
	if len(s) != 1 || !s[0].HasNow || s[0].Now != 9 || !s[0].HasBaseline || s[0].Baseline != 2 {
		t.Fatalf("series = %+v, want now 9 and baseline median 2 (NaN skipped; the mean would be 4)", s)
	}
	if _, ok := s[0].Labels["__name__"]; ok {
		t.Error("__name__ should be dropped")
	}
	for _, c := range src.calls {
		switch {
		case !c.at.IsZero():
			if !c.at.Equal(at) {
				t.Errorf("instant query at %v, want %v", c.at, at)
			}
		default:
			if !c.end.Equal(at.Add(-5*time.Minute)) || !c.start.Equal(at.Add(-35*time.Minute)) || c.step != time.Minute {
				t.Errorf("range = %v..%v step %v", c.start, c.end, c.step)
			}
		}
	}
}

func TestCollect_UnionAndNoData(t *testing.T) {
	src := &fakeSource{
		now: map[string][]metrics.Series{"q": {
			series(map[string]string{"pod": "new"}, 5),
			series(map[string]string{"pod": "nan"}, math.NaN()),
		}},
		before: map[string][]metrics.Series{"q": {series(map[string]string{"pod": "gone"}, 4, 4)}},
	}
	rs, err := Collect(context.Background(), src, []Metric{{Name: "m", Query: "q"}, {Name: "empty", Query: "e"}}, at, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]SeriesReading{}
	for _, s := range rs[0].Series {
		got[s.Labels["pod"]] = s
	}
	if g := got["gone"]; g.HasNow || !g.HasBaseline {
		t.Errorf("gone = %+v, want no data now but a baseline", g)
	}
	if n := got["new"]; !n.HasNow || n.HasBaseline {
		t.Errorf("new = %+v, want now but no baseline", n)
	}
	if n := got["nan"]; n.HasNow {
		t.Errorf("nan = %+v, want NaN treated as no data", n)
	}
	if len(rs[1].Series) != 0 || rs[1].Metric.Name != "empty" {
		t.Errorf("empty metric = %+v", rs[1])
	}
}

func TestCollect_Errors(t *testing.T) {
	src := &fakeSource{err: errors.New("down")}
	_, err := Collect(context.Background(), src, []Metric{{Name: "m", Query: "q"}}, at, time.Minute, time.Hour)
	if err == nil || !strings.Contains(err.Error(), `"m"`) {
		t.Errorf("err = %v, want it to name the metric", err)
	}
	if _, err := Collect(context.Background(), &fakeSource{}, nil, at, 0, time.Hour); err == nil {
		t.Error("zero window: expected error")
	}
}

func TestCollect_KeepsSeriesThatDifferOnlyByName(t *testing.T) {
	src := &fakeSource{now: map[string][]metrics.Series{"q": {
		series(map[string]string{"__name__": "a", "pod": "x"}, 1),
		series(map[string]string{"__name__": "b", "pod": "x"}, 2),
	}}}
	rs, err := Collect(context.Background(), src, []Metric{{Name: "m", Query: "q"}}, at, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs[0].Series) != 2 {
		t.Errorf("series = %+v, want 2 distinct series", rs[0].Series)
	}
}

func TestMedian(t *testing.T) {
	inf := math.Inf(1)
	tests := []struct {
		name string
		vs   []float64
		want float64
		ok   bool
	}{
		{"short incident ignored", []float64{3, 3, 3, 3, 3, 3, 3, 40, 45, 50, 3}, 3, true},
		{"even length", []float64{1, 4}, 2.5, true},
		{"single", []float64{7}, 7, true},
		{"empty", nil, 0, false},
		{"all NaN", []float64{math.NaN()}, 0, false},
		{"all Inf", []float64{inf, -inf}, 0, false},
		{"Inf skipped", []float64{inf, 1, -inf, 5, 3}, 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := median(series(nil, tt.vs...).Samples)
			if ok != tt.ok || (ok && v != tt.want) {
				t.Errorf("median = %v, %v; want %v, %v", v, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestMedian_DoesNotMutateInput(t *testing.T) {
	ss := series(nil, 5, 1, 3).Samples
	before := slices.Clone(ss)
	median(ss)
	if !slices.Equal(ss, before) {
		t.Errorf("input changed: %v -> %v", before, ss)
	}
}
