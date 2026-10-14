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
	byDay  map[int64][]metrics.Series
	dayErr map[int64]error
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
	if f.byDay != nil {
		return f.byDay[start.Unix()], f.dayErr[start.Unix()]
	}
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

func TestCollect_SameSlotLast7Days(t *testing.T) {
	src := &fakeSource{
		now:    map[string][]metrics.Series{"q": {series(map[string]string{"__name__": "x", "pod": "a"}, 9)}},
		before: map[string][]metrics.Series{"q": {series(map[string]string{"__name__": "x", "pod": "a"}, 1, 2, math.NaN(), 9)}},
	}
	rs, err := Collect(context.Background(), metrics.Sources{"": src}, []Metric{{Name: "m", Query: "q"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	s := rs[0].Series
	if len(s) != 1 || !s[0].HasNow || s[0].Now != 9 || !s[0].HasBaseline || s[0].Baseline != 2 {
		t.Fatalf("series = %+v, want now 9 and baseline median 2 (NaN skipped; the mean would be 4)", s)
	}
	if s[0].Days != 7 {
		t.Errorf("days = %d, want 7", s[0].Days)
	}
	if _, ok := s[0].Labels["__name__"]; ok {
		t.Error("__name__ should be dropped")
	}
	var starts []time.Time
	for _, c := range src.calls {
		switch {
		case !c.at.IsZero():
			if !c.at.Equal(at) {
				t.Errorf("instant query at %v, want %v", c.at, at)
			}
		default:
			starts = append(starts, c.start)
			if c.end.Sub(c.start) != time.Hour || c.step != 5*time.Minute {
				t.Errorf("range = %v..%v step %v, want 1h at 5m", c.start, c.end, c.step)
			}
		}
	}
	slices.SortFunc(starts, func(a, b time.Time) int { return b.Compare(a) })
	for d, st := range starts {
		if want := at.In(time.Local).AddDate(0, 0, -(d + 1)).Add(-30 * time.Minute); !st.Equal(want) {
			t.Errorf("slot %d starts %v, want %v", d+1, st, want)
		}
	}
	if len(starts) != 7 {
		t.Errorf("range queries = %d, want 7", len(starts))
	}
}

func TestCollect_TargetSkipsBaseline(t *testing.T) {
	src := &fakeSource{now: map[string][]metrics.Series{"q": {series(nil, 0.3)}}}
	max := 0.01
	rs, err := Collect(context.Background(), metrics.Sources{"": src}, []Metric{{Name: "m", Query: "q", Target: &Target{Max: &max}}}, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.calls) != 1 || rs[0].Series[0].HasBaseline {
		t.Errorf("calls = %+v series = %+v, want only the instant query", src.calls, rs[0].Series)
	}
}

func TestCollect_CountsDaysWithData(t *testing.T) {
	slot := func(d int) int64 { return at.In(time.Local).AddDate(0, 0, -d).Add(-30 * time.Minute).Unix() }
	a := map[string]string{"pod": "a"}
	src := &fakeSource{
		now: map[string][]metrics.Series{"q": {series(a, 5)}},
		byDay: map[int64][]metrics.Series{
			slot(1): {series(a, 1, 1)},
			slot(2): {series(a, 3)},
			slot(3): {series(a, math.NaN())},
		},
	}
	rs, err := Collect(context.Background(), metrics.Sources{"": src}, []Metric{{Name: "m", Query: "q"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	if s := rs[0].Series[0]; s.Days != 2 || s.Baseline != 1 {
		t.Errorf("series = %+v, want 2 days with data (NaN-only day not counted) and median 1 over pooled points", s)
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
	rs, err := Collect(context.Background(), metrics.Sources{"": src}, []Metric{{Name: "m", Query: "q"}, {Name: "empty", Query: "e"}}, at)
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
	_, err := Collect(context.Background(), metrics.Sources{"": src}, []Metric{{Name: "m", Query: "q"}}, at)
	if err == nil || !strings.Contains(err.Error(), `"m"`) {
		t.Errorf("err = %v, want it to name the metric", err)
	}
}

func TestCollect_KeepsSeriesThatDifferOnlyByName(t *testing.T) {
	src := &fakeSource{now: map[string][]metrics.Series{"q": {
		series(map[string]string{"__name__": "a", "pod": "x"}, 1),
		series(map[string]string{"__name__": "b", "pod": "x"}, 2),
	}}}
	rs, err := Collect(context.Background(), metrics.Sources{"": src}, []Metric{{Name: "m", Query: "q"}}, at)
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

func TestCollect_FailedDaysDegrade(t *testing.T) {
	slot := func(d int) int64 { return at.In(time.Local).AddDate(0, 0, -d).Add(-30 * time.Minute).Unix() }
	src := &fakeSource{
		now:    map[string][]metrics.Series{"q": {series(nil, 5)}},
		byDay:  map[int64][]metrics.Series{slot(1): {series(nil, 4)}},
		dayErr: map[int64]error{},
	}
	for d := 2; d <= 7; d++ {
		src.dayErr[slot(d)] = errors.New("timeout")
	}
	rs, err := Collect(context.Background(), metrics.Sources{"": src}, []Metric{{Name: "m", Query: "q"}}, at)
	if err != nil {
		t.Fatalf("one good day must be enough: %v", err)
	}
	if s := rs[0].Series[0]; s.Days != 1 || s.Baseline != 4 {
		t.Errorf("series = %+v, want 1 day, baseline 4", s)
	}
	src.dayErr[slot(1)] = errors.New("timeout")
	if _, err := Collect(context.Background(), metrics.Sources{"": src}, []Metric{{Name: "m", Query: "q"}}, at); err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Errorf("all days failed: err = %v, want a baseline error", err)
	}
}

func TestCollect_DispatchesBySource(t *testing.T) {
	prom := &fakeSource{now: map[string][]metrics.Series{"q": {series(nil, 1)}}}
	dd := &fakeSource{now: map[string][]metrics.Series{"q": {series(nil, 2)}}}
	srcs := metrics.Sources{"prometheus": prom, "datadog": dd}
	ms := []Metric{
		{Name: "a", Source: "prometheus", Query: "q", Target: &Target{Max: f64(5)}},
		{Name: "b", Source: "datadog", Query: "q", Target: &Target{Max: f64(5)}},
	}
	rs, err := Collect(context.Background(), srcs, ms, at)
	if err != nil {
		t.Fatal(err)
	}
	if rs[0].Series[0].Now != 1 || rs[1].Series[0].Now != 2 || len(prom.calls) != 1 || len(dd.calls) != 1 {
		t.Errorf("readings = %+v, prom calls = %d, datadog calls = %d", rs, len(prom.calls), len(dd.calls))
	}
	ms[1].Source = "graphite"
	if _, err := Collect(context.Background(), srcs, ms, at); err == nil || !strings.Contains(err.Error(), `unknown source "graphite"`) {
		t.Errorf("err = %v, want unknown source", err)
	}
}
