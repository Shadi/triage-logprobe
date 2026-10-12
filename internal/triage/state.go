package triage

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

const stateIntro = "Each metric shows its current value. In brackets is its target if it has one, otherwise its normal value: the median at this time of day over the previous 7 days."

const (
	maxSeriesPerMetric = 5
	maxLabelRunes      = 40
	maxRatio           = 1000
	minRatioBaseline   = 1e-6
)

func RenderState(rs []Reading) string {
	var sb strings.Builder
	sb.WriteString(stateIntro + "\n")
	for _, r := range rs {
		if len(r.Series) == 0 {
			fmt.Fprintf(&sb, "%s: %s\n", r.Metric.Name, describe(r.Metric, SeriesReading{}))
			continue
		}
		varying := varyingLabels(r.Series)
		for i, s := range rankByAnomaly(r.Metric.Target, r.Series) {
			if i == maxSeriesPerMetric {
				fmt.Fprintf(&sb, "%s: +%d more series\n", r.Metric.Name, len(r.Series)-i)
				break
			}
			fmt.Fprintf(&sb, "%s\n", oneLine(r.Metric.Name+labelSuffix(s.Labels, varying)+": "+describe(r.Metric, s)))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func describe(m Metric, s SeriesReading) string {
	if m.Target != nil {
		return describeTarget(m.Name, *m.Target, s)
	}
	d := describeNormal(m.Name, s)
	if s.HasBaseline && s.Days > 0 && s.Days < baselineDays {
		d = strings.TrimSuffix(d, ")") + fmt.Sprintf(", from %d of %d days)", s.Days, baselineDays)
	}
	return d
}

func describeNormal(name string, s SeriesReading) string {
	f := func(v float64) string { return format(name, v) }
	switch {
	case !s.HasNow && !s.HasBaseline:
		return "no data now, no data before"
	case !s.HasNow:
		return fmt.Sprintf("no data now (normal %s)", f(s.Baseline))
	case !s.HasBaseline:
		return fmt.Sprintf("%s (new, no data before)", f(s.Now))
	case s.Now == 0 && s.Baseline > 0:
		return fmt.Sprintf("%s (normal %s, dropped to zero)", f(0), f(s.Baseline))
	case s.Now > 0 && s.Baseline >= minRatioBaseline:
		ratio := s.Now / s.Baseline
		if ratio > maxRatio {
			return fmt.Sprintf("%s (normal %s, more than %d× normal)", f(s.Now), f(s.Baseline), maxRatio)
		}
		return fmt.Sprintf("%s (normal %s, %s× normal)", f(s.Now), f(s.Baseline), num(ratio))
	default:
		return fmt.Sprintf("%s (normal %s)", f(s.Now), f(s.Baseline))
	}
}

func format(name string, v float64) string {
	if strings.HasSuffix(name, "_ratio") {
		return num(v*100) + "%"
	}
	return num(v)
}

func num(v float64) string {
	if v == 0 {
		return "0"
	}
	if math.Abs(v) >= 1000 {
		return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 3, 64), 64)
	if math.Abs(r) >= 1000 {
		return strconv.FormatFloat(math.Round(r), 'f', 0, 64)
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

func describeTarget(name string, t Target, s SeriesReading) string {
	f := func(v float64) string { return format(name, v) }
	var target string
	switch {
	case t.Min != nil && t.Max != nil:
		target = fmt.Sprintf("target %s to %s", f(*t.Min), f(*t.Max))
	case t.Max != nil:
		target = "target at most " + f(*t.Max)
	default:
		target = "target at least " + f(*t.Min)
	}
	switch {
	case !s.HasNow:
		return fmt.Sprintf("no data now (%s)", target)
	case t.Max != nil && s.Now > *t.Max:
		switch r := s.Now / *t.Max; {
		case *t.Max > 0 && r > maxRatio:
			return fmt.Sprintf("%s (%s, above target, more than %d× the limit)", f(s.Now), target, maxRatio)
		case *t.Max > 0 && r >= 2:
			return fmt.Sprintf("%s (%s, above target, %s× the limit)", f(s.Now), target, num(r))
		}
		return fmt.Sprintf("%s (%s, above target)", f(s.Now), target)
	case t.Min != nil && s.Now < *t.Min:
		return fmt.Sprintf("%s (%s, below target)", f(s.Now), target)
	default:
		return fmt.Sprintf("%s (%s, within target)", f(s.Now), target)
	}
}

func breach(t Target, s SeriesReading) float64 {
	switch {
	case !s.HasNow:
		return 1
	case t.Max != nil && s.Now > *t.Max:
		return 1 + (s.Now-*t.Max)/math.Max(math.Abs(*t.Max), minRatioBaseline)
	case t.Min != nil && s.Now < *t.Min:
		return 1 + (*t.Min-s.Now)/math.Max(math.Abs(*t.Min), minRatioBaseline)
	default:
		return 0
	}
}

func anomaly(s SeriesReading) float64 {
	switch {
	case !s.HasNow && s.HasBaseline:
		return math.Inf(1)
	case !s.HasNow:
		return 0
	case !s.HasBaseline:
		return 1
	default:
		return math.Abs(s.Now-s.Baseline) / math.Max(math.Abs(s.Baseline), minRatioBaseline)
	}
}

func rankByAnomaly(t *Target, ss []SeriesReading) []SeriesReading {
	score := anomaly
	if t != nil {
		score = func(s SeriesReading) float64 { return breach(*t, s) }
	}
	out := slices.Clone(ss)
	slices.SortStableFunc(out, func(a, b SeriesReading) int {
		return cmp.Compare(score(b), score(a))
	})
	return out
}

func sanitize(s string) string {
	s = strings.NewReplacer("<", "‹", ">", "›", `"`, "'").Replace(oneLine(s))
	if r := []rune(s); len(r) > maxLabelRunes {
		s = string(r[:maxLabelRunes]) + "…"
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func varyingLabels(ss []SeriesReading) []string {
	if len(ss) < 2 {
		return nil
	}
	all := map[string]bool{}
	for _, s := range ss {
		for k := range s.Labels {
			all[k] = true
		}
	}
	var out []string
	for _, k := range slices.Sorted(maps.Keys(all)) {
		first, same := ss[0].Labels[k], true
		for _, s := range ss[1:] {
			if s.Labels[k] != first {
				same = false
				break
			}
		}
		if !same {
			out = append(out, k)
		}
	}
	return out
}

func labelSuffix(l map[string]string, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = sanitize(k) + `="` + sanitize(l[k]) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}
