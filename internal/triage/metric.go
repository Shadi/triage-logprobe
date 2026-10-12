package triage

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type Target struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

type Metric struct {
	Name   string  `json:"name"`
	Query  string  `json:"query"`
	Target *Target `json:"target,omitempty"`
}

var (
	metricName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	metricLine = regexp.MustCompile(`^([^=\[]*?)\s*(?:\[([^\]]*)\])?\s*=(.*)$`)
)

func ParseMetric(s string) (Metric, error) {
	line := strings.TrimSpace(s)
	idx := metricLine.FindStringSubmatchIndex(line)
	if idx == nil || strings.TrimSpace(line[idx[6]:idx[7]]) == "" {
		return Metric{}, fmt.Errorf("metric %q: want name=promql or name[max=N]=promql", s)
	}
	name, query := line[idx[2]:idx[3]], strings.TrimSpace(line[idx[6]:idx[7]])
	if !metricName.MatchString(name) {
		return Metric{}, fmt.Errorf("metric name %q: want lowercase letters, digits and underscores", name)
	}
	out := Metric{Name: name, Query: query}
	if idx[4] >= 0 {
		t, err := parseTarget(line[idx[4]:idx[5]])
		if err != nil {
			return Metric{}, fmt.Errorf("metric %q target: %w", name, err)
		}
		out.Target = &t
	}
	return out, nil
}

func parseTarget(s string) (Target, error) {
	var t Target
	for part := range strings.SplitSeq(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok {
			return t, fmt.Errorf("%q: want min=N or max=N", part)
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return t, fmt.Errorf("%q: not a finite number", v)
		}
		switch {
		case k == "min" && t.Min == nil:
			t.Min = &f
		case k == "max" && t.Max == nil:
			t.Max = &f
		default:
			return t, fmt.Errorf("%q: want each of min and max at most once", part)
		}
	}
	if t.Min != nil && t.Max != nil && *t.Min > *t.Max {
		return t, fmt.Errorf("min %v is above max %v", *t.Min, *t.Max)
	}
	return t, nil
}

func ParseMetrics(r io.Reader) ([]Metric, error) {
	var out []Metric
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m, err := ParseMetric(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		out = append(out, m)
	}
	return out, sc.Err()
}

func CheckUnique(ms []Metric) error {
	seen := make(map[string]bool, len(ms))
	for _, m := range ms {
		if seen[m.Name] {
			return fmt.Errorf("duplicate metric name %q", m.Name)
		}
		seen[m.Name] = true
	}
	return nil
}
