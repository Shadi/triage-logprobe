package triage

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/shadi/triage-logprobe/internal/metrics"
)

const (
	baselinePoints       = 30
	maxConcurrentMetrics = 8
)

type SeriesReading struct {
	Labels      map[string]string `json:"labels"`
	Now         float64           `json:"now"`
	Baseline    float64           `json:"baseline"`
	HasNow      bool              `json:"has_now"`
	HasBaseline bool              `json:"has_baseline"`
}

type Reading struct {
	Metric Metric          `json:"metric"`
	Series []SeriesReading `json:"series"`
}

func Collect(ctx context.Context, src metrics.Source, ms []Metric, at time.Time, window, baseline time.Duration) ([]Reading, error) {
	if window <= 0 || baseline <= 0 {
		return nil, fmt.Errorf("window and baseline must be positive")
	}
	end := at.Add(-window)
	start := end.Add(-baseline)
	step := max(baseline/baselinePoints, 15*time.Second)

	out := make([]Reading, len(ms))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentMetrics)
	for i, m := range ms {
		g.Go(func() error {
			now, err := src.Query(gctx, m.Query, at)
			if err != nil {
				return fmt.Errorf("metric %q: %w", m.Name, err)
			}
			before, err := src.QueryRange(gctx, m.Query, start, end, step)
			if err != nil {
				return fmt.Errorf("metric %q baseline range query: %w", m.Name, err)
			}
			out[i] = Reading{Metric: m, Series: merge(now, before)}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func merge(now, before []metrics.Series) []SeriesReading {
	byKey := map[string]*SeriesReading{}
	get := func(l map[string]string) *SeriesReading {
		k := labelKey(l)
		if r, ok := byKey[k]; ok {
			return r
		}
		r := &SeriesReading{Labels: withoutName(l)}
		byKey[k] = r
		return r
	}
	for _, s := range now {
		r := get(s.Labels)
		if len(s.Samples) > 0 && finite(s.Samples[len(s.Samples)-1].V) {
			r.Now, r.HasNow = s.Samples[len(s.Samples)-1].V, true
		}
	}
	for _, s := range before {
		r := get(s.Labels)
		if v, ok := median(s.Samples); ok {
			r.Baseline, r.HasBaseline = v, true
		}
	}
	out := make([]SeriesReading, 0, len(byKey))
	for _, k := range slices.Sorted(maps.Keys(byKey)) {
		out = append(out, *byKey[k])
	}
	return out
}

func median(ss []metrics.Sample) (float64, bool) {
	vs := make([]float64, 0, len(ss))
	for _, s := range ss {
		if finite(s.V) {
			vs = append(vs, s.V)
		}
	}
	if len(vs) == 0 {
		return 0, false
	}
	slices.Sort(vs)
	mid := len(vs) / 2
	if len(vs)%2 == 1 {
		return vs[mid], true
	}
	return (vs[mid-1] + vs[mid]) / 2, true
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func withoutName(l map[string]string) map[string]string {
	out := make(map[string]string, len(l))
	for k, v := range l {
		if k != "__name__" {
			out[k] = v
		}
	}
	return out
}

func labelKey(l map[string]string) string {
	keys := slices.Sorted(maps.Keys(l))
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + l[k]
	}
	return strings.Join(parts, ",")
}
