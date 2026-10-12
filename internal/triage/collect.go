package triage

import (
	"context"
	"errors"
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
	baselineDays         = 7
	baselineSlot         = 30 * time.Minute
	baselineStep         = 5 * time.Minute
	maxConcurrentQueries = 8
)

type SeriesReading struct {
	Labels      map[string]string `json:"labels"`
	Now         float64           `json:"now"`
	Baseline    float64           `json:"baseline"`
	HasNow      bool              `json:"has_now"`
	HasBaseline bool              `json:"has_baseline"`
	Days        int               `json:"baseline_days,omitempty"`
}

type Reading struct {
	Metric Metric          `json:"metric"`
	Series []SeriesReading `json:"series"`
}

func Collect(ctx context.Context, src metrics.Source, ms []Metric, at time.Time) ([]Reading, error) {
	local := at.In(time.Local)
	now := make([][]metrics.Series, len(ms))
	days := make([][][]metrics.Series, len(ms))
	dayErrs := make([][]error, len(ms))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentQueries)
	for i, m := range ms {
		g.Go(func() error {
			ss, err := src.Query(gctx, m.Query, at)
			if err != nil {
				return fmt.Errorf("metric %q: %w", m.Name, err)
			}
			now[i] = ss
			return nil
		})
		if m.Target != nil {
			continue
		}
		days[i] = make([][]metrics.Series, baselineDays)
		dayErrs[i] = make([]error, baselineDays)
		for d := range baselineDays {
			g.Go(func() error {
				day := local.AddDate(0, 0, -(d + 1))
				days[i][d], dayErrs[i][d] = src.QueryRange(gctx, m.Query, day.Add(-baselineSlot), day.Add(baselineSlot), baselineStep)
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := make([]Reading, len(ms))
	for i, m := range ms {
		if dayErrs[i] != nil && !slices.ContainsFunc(dayErrs[i], func(e error) bool { return e == nil }) {
			return nil, fmt.Errorf("metric %q baseline: %w", m.Name, errors.Join(dayErrs[i]...))
		}
		out[i] = Reading{Metric: m, Series: merge(now[i], days[i])}
	}
	return out, nil
}

func merge(now []metrics.Series, days [][]metrics.Series) []SeriesReading {
	byKey := map[string]*SeriesReading{}
	pooled := map[string][]metrics.Sample{}
	seen := map[string]int{}
	get := func(l map[string]string) (string, *SeriesReading) {
		k := labelKey(l)
		if r, ok := byKey[k]; ok {
			return k, r
		}
		r := &SeriesReading{Labels: withoutName(l)}
		byKey[k] = r
		return k, r
	}
	for _, s := range now {
		_, r := get(s.Labels)
		if len(s.Samples) > 0 && finite(s.Samples[len(s.Samples)-1].V) {
			r.Now, r.HasNow = s.Samples[len(s.Samples)-1].V, true
		}
	}
	for _, day := range days {
		counted := map[string]bool{}
		for _, s := range day {
			k, _ := get(s.Labels)
			pooled[k] = append(pooled[k], s.Samples...)
			if !counted[k] && slices.ContainsFunc(s.Samples, func(x metrics.Sample) bool { return finite(x.V) }) {
				counted[k] = true
				seen[k]++
			}
		}
	}
	for k, ss := range pooled {
		if v, ok := median(ss); ok {
			byKey[k].Baseline, byKey[k].HasBaseline, byKey[k].Days = v, true, seen[k]
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
